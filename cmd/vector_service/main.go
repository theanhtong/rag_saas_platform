package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/theanhtong/rag_system/internal/repository"
	"github.com/theanhtong/rag_system/internal/server"
	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

func main() {
	port := getEnv("VECTOR_SERVICE_PORT", "50051")
	pgHost := getEnv("POSTGRES_HOST", "localhost")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgUser := getEnv("POSTGRES_USER", "rag_user")
	pgPass := getEnv("POSTGRES_PASSWORD", "rag_password")
	pgDB := getEnv("POSTGRES_DB", "rag_db")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		pgHost, pgPort, pgUser, pgPass, pgDB)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("database ping check failed: %v", err)
	}
	log.Println("successfully connected to PostgreSQL pgvector database.")

	// initialize repository and gRPC server layers
	vectorRepo := repository.NewVectorRepository(db)
	vectorServer := server.NewVectorServer(vectorRepo)

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	pbv1.RegisterVectorServiceServer(grpcServer, vectorServer)
	reflection.Register(grpcServer)

	go func() {
		log.Printf("starting gRPC Vector Service on port %s...", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC vector service: %v", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan

	log.Println("shutting down gRPC Vector Service...")
	grpcServer.GracefulStop()
	log.Println("gRPC Vector Service stopped successfully.")
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
