package ingest

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	ListDocuments() []DocumentMetadata
}

type ingestionPipeline struct {
	chunker      Chunker
	embedder     embedder.EmbeddingService
	vectorClient client.VectorClient
	mu           sync.RWMutex
	docs         map[string]DocumentMetadata
}

// NewPipeline constructs a new Ingestion Pipeline instance.
func NewPipeline(c Chunker, emb embedder.EmbeddingService, vc client.VectorClient) Pipeline {
	return &ingestionPipeline{
		chunker:      c,
		embedder:     emb,
		vectorClient: vc,
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
		vec, err := p.embedder.EmbedText(ctx, ch.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for chunk %s: %w", ch.ID, err)
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
			// Log warning or propagate error if vector service is mandatory
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

	return &IngestionResult{
		DocumentID:    actualDocID,
		Filename:      filename,
		InsertedCount: insertedCount,
		Success:       true,
	}, nil
}

func (p *ingestionPipeline) ListDocuments() []DocumentMetadata {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]DocumentMetadata, 0, len(p.docs))
	for _, doc := range p.docs {
		result = append(result, doc)
	}
	return result
}
