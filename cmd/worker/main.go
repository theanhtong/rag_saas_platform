package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/client"
	"github.com/theanhtong/rag_system/internal/config"
	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/ingest"
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

	// initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// initialize gRPC Vector Service client
	vectorClient, err := client.NewVectorClient(&cfg.VectorService)
	if err != nil {
		log.Printf("[WARN] worker vector service connection init deferred: %v", err)
	} else {
		defer func() {
			_ = vectorClient.Close()
		}()
	}

	// initialize core components
	embeddingService := embedder.NewEmbeddingService(&cfg.Embedding)
	chunker := ingest.NewChunker()
	ingestionPipeline := ingest.NewPipeline(chunker, embeddingService, vectorClient, rdb)

	// initialize Asynq queue client and processor
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer asynqClient.Close()

	ingestQueue := ingest.NewIngestQueue(asynqClient, rdb)
	processor := ingest.NewTaskProcessor(ingestionPipeline, ingestQueue)

	// initialize Asynq server
	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		},
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"default": 10,
			},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(ingest.TypeDocumentIngest, processor.ProcessIngestTask)

	go func() {
		log.Println("starting background ingestion worker server...")
		if err := srv.Run(mux); err != nil {
			log.Fatalf("asynq worker server error: %v", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan

	log.Println("initiating graceful worker shutdown...")
	srv.Shutdown()
	log.Println("background ingestion worker server stopped.")
}
