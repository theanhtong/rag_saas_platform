package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/cache"
	"github.com/theanhtong/rag_system/internal/client"
	"github.com/theanhtong/rag_system/internal/config"
	"github.com/theanhtong/rag_system/internal/handler"
	"github.com/theanhtong/rag_system/internal/middleware"
	"github.com/theanhtong/rag_system/internal/router"
)

func main() {
	configPath := "config.yaml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	// initialize Redis client for rate limiting and semantic caching
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// initialize gRPC Vector Service client
	vectorClient, err := client.NewVectorClient(&cfg.VectorService)
	if err != nil {
		log.Printf("[WARN] vector service connection init deferred: %v", err)
	} else {
		defer func() {
			_ = vectorClient.Close()
		}()
	}

	// initialize core components
	semanticCache := cache.NewSemanticCache(rdb, &cfg.SemanticCache)
	providerRouter := router.NewProviderRouter(&cfg.Providers)

	// initialize HTTP handlers and rate limiter
	chatHandler := handler.NewChatHandler(vectorClient, semanticCache, providerRouter)
	rateLimiter := middleware.NewRateLimiter(rdb, &cfg.RateLimit)

	// setup Gin engine and global middlewares
	engine := gin.New()
	engine.Use(middleware.LoggerMiddleware())
	engine.Use(middleware.CORSMiddleware())
	engine.Use(gin.Recovery())

	// health check endpoint
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
		})
	})

	// version 1 protected API routes
	v1 := engine.Group("/v1")
	v1.Use(rateLimiter.Middleware())
	{
		v1.POST("/chat/completions", chatHandler.HandleChatCompletions)
	}

	serverAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:    serverAddr,
		Handler: engine,
	}

	go func() {
		log.Printf("starting LLM API Gateway server on %s", serverAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("gateway server execution error: %v", err)
		}
	}()

	// graceful shutdown listening signal
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan

	log.Println("initiating graceful server shutdown...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced shutdown error: %v", err)
	}

	log.Println("LLM API Gateway server successfully stopped.")
}
