package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/client"
	"github.com/theanhtong/rag_system/internal/embedder"
	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

// DocumentMetadata stores high-level info about an ingested document.
type DocumentMetadata struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	NumChunks int       `json:"num_chunks"`
	CreatedAt time.Time `json:"created_at"`
}

// IngestionResult summarizes the ingestion execution status.
type IngestionResult struct {
	DocumentID    string `json:"document_id"`
	Filename      string `json:"filename"`
	InsertedCount int    `json:"inserted_count"`
	Success       bool   `json:"success"`
}

// Pipeline manages end-to-end document chunking, embedding, and vector index storage.
type Pipeline interface {
	IngestDocument(ctx context.Context, documentID string, filename string, rawText string, cfg ChunkingConfig) (*IngestionResult, error)
	ListDocuments(ctx context.Context) []DocumentMetadata
}

type ingestionPipeline struct {
	chunker      Chunker
	embedder     embedder.EmbeddingService
	vectorClient client.VectorClient
	rdb          redis.UniversalClient
	mu           sync.RWMutex
	docs         map[string]DocumentMetadata
}

// NewPipeline constructs a new Ingestion Pipeline instance.
func NewPipeline(c Chunker, emb embedder.EmbeddingService, vc client.VectorClient, rdb redis.UniversalClient) Pipeline {
	return &ingestionPipeline{
		chunker:      c,
		embedder:     emb,
		vectorClient: vc,
		rdb:          rdb,
		docs:         make(map[string]DocumentMetadata),
	}
}

func (p *ingestionPipeline) IngestDocument(ctx context.Context, documentID string, filename string, rawText string, cfg ChunkingConfig) (*IngestionResult, error) {
	chunks := p.chunker.SplitText(documentID, filename, rawText, cfg)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("empty text provided or chunking yielded 0 chunks")
	}

	actualDocID := chunks[0].DocumentID

	var records []*pbv1.VectorRecord
	for _, ch := range chunks {
		var vec []float32
		if p.embedder != nil {
			var err error
			vec, err = p.embedder.EmbedText(ctx, ch.Content)
			if err != nil {
				return nil, fmt.Errorf("failed to generate embedding for chunk %s: %w", ch.ID, err)
			}
		}

		meta := make(map[string]string)
		for k, v := range ch.Metadata {
			meta[k] = v
		}
		meta["content"] = ch.Content

		records = append(records, &pbv1.VectorRecord{
			Id:       ch.ID,
			Values:   vec,
			Metadata: meta,
		})
	}

	insertedCount := len(records)
	if p.vectorClient != nil {
		resp, err := p.vectorClient.InsertBatch(ctx, records)
		if err != nil {
			fmt.Printf("[WARN] vector service InsertBatch failed: %v\n", err)
		} else if resp != nil {
			insertedCount = int(resp.InsertedCount)
		}
	}

	docMeta := DocumentMetadata{
		ID:        actualDocID,
		Filename:  filename,
		NumChunks: len(chunks),
		CreatedAt: time.Now(),
	}

	p.mu.Lock()
	p.docs[actualDocID] = docMeta
	p.mu.Unlock()

	// persist metadata in Redis Hash for cross-process synchronization
	if p.rdb != nil {
		bytes, err := json.Marshal(docMeta)
		if err == nil {
			_ = p.rdb.HSet(ctx, "documents:metadata", actualDocID, bytes).Err()
		}
	}

	return &IngestionResult{
		DocumentID:    actualDocID,
		Filename:      filename,
		InsertedCount: insertedCount,
		Success:       true,
	}, nil
}

func (p *ingestionPipeline) ListDocuments(ctx context.Context) []DocumentMetadata {
	if p.rdb != nil {
		vals, err := p.rdb.HGetAll(ctx, "documents:metadata").Result()
		if err == nil && len(vals) > 0 {
			result := make([]DocumentMetadata, 0, len(vals))
			for _, val := range vals {
				var doc DocumentMetadata
				if err := json.Unmarshal([]byte(val), &doc); err == nil {
					result = append(result, doc)
				}
			}
			return result
		}
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]DocumentMetadata, 0, len(p.docs))
	for _, doc := range p.docs {
		result = append(result, doc)
	}
	return result
}
