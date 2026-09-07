package ingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextChunker_SplitText(t *testing.T) {
	chunker := NewChunker()

	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString(fmt.Sprintf("Line %d of enterprise knowledge base document. ", i))
	}
	text := sb.String()

	cfg := ChunkingConfig{
		ChunkSize:    200,
		ChunkOverlap: 40,
	}

	chunks := chunker.SplitText("doc-123", "policy.txt", text, cfg)
	require.NotEmpty(t, chunks)

	for i, ch := range chunks {
		assert.Equal(t, "doc-123", ch.DocumentID)
		assert.Equal(t, "policy.txt", ch.Filename)
		assert.Equal(t, i, ch.ChunkIndex)
		assert.NotEmpty(t, ch.Content)
		assert.Contains(t, ch.ID, "doc-123-chunk-")
		assert.Equal(t, fmt.Sprintf("%d", i), ch.Metadata["chunk_index"])
	}
}

func TestTextChunker_EmptyText(t *testing.T) {
	chunker := NewChunker()
	chunks := chunker.SplitText("doc-123", "empty.txt", "   ", DefaultChunkingConfig())
	assert.Empty(t, chunks)
}
