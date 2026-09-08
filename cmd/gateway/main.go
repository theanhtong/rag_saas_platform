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
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/cache"
	"github.com/theanhtong/rag_system/internal/client"
	"github.com/theanhtong/rag_system/internal/config"
	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/handler"
	"github.com/theanhtong/rag_system/internal/ingest"
	"github.com/theanhtong/rag_system/internal/middleware"
	"github.com/theanhtong/rag_system/internal/router"
	"github.com/theanhtong/rag_system/internal/service"
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
	embeddingService := embedder.NewEmbeddingService(&cfg.Embedding)
	semanticCache := cache.NewSemanticCache(rdb, &cfg.SemanticCache)
	providerRouter := router.NewProviderRouter(&cfg.Providers)

	// initialize ingestion pipeline
	chunker := ingest.NewChunker()
	ingestionPipeline := ingest.NewPipeline(chunker, embeddingService, vectorClient, rdb)

	// initialize asynq queue client for background task management
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer asynqClient.Close()

	ingestQueue := ingest.NewIngestQueue(asynqClient, rdb)

	// initialize HTTP handlers, quota service and rate limiter
	quotaService := service.NewQuotaService(rdb)
	ragService := service.NewRAGService(vectorClient)
	chatHandler := handler.NewChatHandler(vectorClient, semanticCache, providerRouter, embeddingService, ragService)
	docHandler := handler.NewDocumentHandler(ingestionPipeline, ingestQueue, semanticCache)
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
	v1.Use(middleware.MultiTenantAuthMiddleware(quotaService))
	v1.Use(rateLimiter.Middleware())
	{
		v1.POST("/chat/completions", chatHandler.HandleChatCompletions)
		v1.POST("/documents/ingest", docHandler.HandleIngest)
		v1.GET("/documents/tasks/:id", docHandler.HandleGetTaskStatus)
		v1.GET("/documents", docHandler.HandleListDocuments)
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
