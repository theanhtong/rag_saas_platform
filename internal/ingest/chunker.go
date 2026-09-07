package ingest

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// DocumentChunk represents a single text chunk with positional metadata.
type DocumentChunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	Filename   string            `json:"filename"`
	ChunkIndex int               `json:"chunk_index"`
	PageNumber int               `json:"page_number"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata"`
}

// ChunkingConfig holds parameters for the sliding window text splitting algorithm.
type ChunkingConfig struct {
	ChunkSize    int // default: 500 characters
	ChunkOverlap int // default: 50 characters
}

// DefaultChunkingConfig returns sensible default chunking settings.
func DefaultChunkingConfig() ChunkingConfig {
	return ChunkingConfig{
		ChunkSize:    500,
		ChunkOverlap: 50,
	}
}

// Chunker handles sliding window document text splitting.
type Chunker interface {
	SplitText(documentID string, filename string, rawText string, cfg ChunkingConfig) []DocumentChunk
}

type textChunker struct{}

// NewChunker creates a new Chunker instance.
func NewChunker() Chunker {
	return &textChunker{}
}

func (c *textChunker) SplitText(documentID string, filename string, rawText string, cfg ChunkingConfig) []DocumentChunk {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 500
	}
	if cfg.ChunkOverlap < 0 || cfg.ChunkOverlap >= cfg.ChunkSize {
		cfg.ChunkOverlap = 50
	}

	cleanText := strings.TrimSpace(rawText)
	if len(cleanText) == 0 {
		return nil
	}

	if documentID == "" {
		hash := sha256.Sum256([]byte(filename + cleanText))
		documentID = fmt.Sprintf("doc-%x", hash[:8])
	}

	var chunks []DocumentChunk
	runes := []rune(cleanText)
	totalRunes := len(runes)
	step := cfg.ChunkSize - cfg.ChunkOverlap
	chunkIndex := 0

	for start := 0; start < totalRunes; start += step {
		end := start + cfg.ChunkSize
		if end > totalRunes {
			end = totalRunes
		}

		chunkText := strings.TrimSpace(string(runes[start:end]))
		if len(chunkText) == 0 {
			continue
		}

		chunkID := fmt.Sprintf("%s-chunk-%d", documentID, chunkIndex)
		meta := map[string]string{
			"document_id": documentID,
			"filename":    filename,
			"chunk_index": fmt.Sprintf("%d", chunkIndex),
			"char_start":  fmt.Sprintf("%d", start),
			"char_end":    fmt.Sprintf("%d", end),
			"created_at":  time.Now().Format(time.RFC3339),
		}

		chunks = append(chunks, DocumentChunk{
			ID:         chunkID,
			DocumentID: documentID,
			Filename:   filename,
			ChunkIndex: chunkIndex,
			PageNumber: 1,
			Content:    chunkText,
			Metadata:   meta,
		})

		chunkIndex++
		if end == totalRunes {
			break
		}
	}

	return chunks
}
