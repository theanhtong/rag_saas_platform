package embedder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/config"
)

func TestEmbeddingService_Fallback(t *testing.T) {
	cfg := &config.EmbeddingConfig{
		Provider:   "mock",
		Model:      "test-model",
		Dimensions: 128,
	}

	service := NewEmbeddingService(cfg)
	assert.Equal(t, 128, service.Dimensions())

	vec, err := service.EmbedText(context.Background(), "Hello enterprise RAG system")
	require.NoError(t, err)
	assert.Equal(t, 128, len(vec))

	// Verify normalization
	var norm float64
	for _, val := range vec {
		norm += float64(val) * float64(val)
	}
	assert.InDelta(t, 1.0, norm, 1e-4)
}

func TestEmbeddingService_EmptyText(t *testing.T) {
	cfg := &config.EmbeddingConfig{
		Provider:   "ollama",
		Dimensions: 384,
	}

	service := NewEmbeddingService(cfg)
	vec, err := service.EmbedText(context.Background(), "   ")
	require.NoError(t, err)
	assert.Equal(t, 384, len(vec))
}
