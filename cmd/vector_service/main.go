package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"google.golang.org/grpc"

	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

type server struct {
	pbv1.UnimplementedVectorServiceServer
	db *sql.DB
}

func main() {
	dbHost := getEnv("POSTGRES_HOST", "localhost")
	dbPort := getEnv("POSTGRES_PORT", "5432")
	dbUser := getEnv("POSTGRES_USER", "rag_user")
	dbPass := getEnv("POSTGRES_PASSWORD", "rag_password")
	dbName := getEnv("POSTGRES_DB", "rag_db")
	port := getEnv("PORT", "50051")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Printf("warning: initial postgres ping failed: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	s := &server{db: db}
	pbv1.RegisterVectorServiceServer(grpcServer, s)

	go func() {
		log.Printf("starting gRPC Vector Service on port :%s", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan

	log.Println("shutting down gRPC Vector Service...")
	grpcServer.GracefulStop()
	log.Println("gRPC Vector Service stopped.")
}

func (s *server) SearchSimilarVectors(ctx context.Context, req *pbv1.SearchRequest) (*pbv1.SearchResponse, error) {
	start := time.Now()
	if len(req.GetVector()) == 0 {
		return &pbv1.SearchResponse{Results: nil, ExecutionTimeUs: 0}, nil
	}

	topK := req.GetTopK()
	if topK <= 0 {
		topK = 5
	}

	vectorStr := float32SliceToString(req.GetVector())

	query := `
		SELECT id, 1 - (embedding <=> $1) AS score, metadata
		FROM vector_records
		ORDER BY embedding <=> $1
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, vectorStr, topK)
	if err != nil {
		log.Printf("search query error: %v", err)
		return nil, fmt.Errorf("vector search query failed: %w", err)
	}
	defer rows.Close()

	var results []*pbv1.SearchResult
	for rows.Next() {
		var id string
		var score float32
		var metadataJSON []byte

		if err := rows.Scan(&id, &score, &metadataJSON); err != nil {
			log.Printf("row scan error: %v", err)
			continue
		}

		metadata := make(map[string]string)
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &metadata)
		}

		results = append(results, &pbv1.SearchResult{
			Id:       id,
			Score:    score,
			Vector:   req.GetVector(),
			Metadata: metadata,
		})
	}

	elapsed := time.Since(start).Microseconds()
	return &pbv1.SearchResponse{
		Results:         results,
		ExecutionTimeUs: elapsed,
	}, nil
}

func (s *server) InsertBatch(ctx context.Context, req *pbv1.InsertBatchRequest) (*pbv1.InsertBatchResponse, error) {
	records := req.GetRecords()
	if len(records) == 0 {
		return &pbv1.InsertBatchResponse{InsertedCount: 0, Success: true}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vector_records (id, embedding, metadata, content_tsvector)
		VALUES ($1, $2, $3, to_tsvector('english', COALESCE($4, '')))
		ON CONFLICT (id) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			metadata = EXCLUDED.metadata,
			content_tsvector = EXCLUDED.content_tsvector,
			created_at = NOW()
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	var insertedCount int32
	for _, rec := range records {
		vectorStr := float32SliceToString(rec.GetValues())
		metadataJSON, _ := json.Marshal(rec.GetMetadata())

		textChunk := rec.GetMetadata()["text"]

		_, err := stmt.ExecContext(ctx, rec.GetId(), vectorStr, metadataJSON, textChunk)
		if err != nil {
			log.Printf("failed to insert record %s: %v", rec.GetId(), err)
			continue
		}
		insertedCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &pbv1.InsertBatchResponse{
		InsertedCount: insertedCount,
		Success:       true,
	}, nil
}

func float32SliceToString(slice []float32) string {
	strs := make([]string, len(slice))
	for i, v := range slice {
		strs[i] = strconv.FormatFloat(float64(v), 'f', 6, 32)
	}
	return "[" + strings.Join(strs, ",") + "]"
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
