package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/config"
)

func TestProviderRouter_FallbackToLocalSLM(t *testing.T) {
	cfg := &config.ProvidersConfig{
		OpenAIAPIKey:   "",
		GeminiAPIKey:   "",
		TimeoutSeconds: 1,
	}

	router := NewProviderRouter(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, providerUsed, err := router.GenerateStream(ctx, "Hello LLM")
	require.NoError(t, err)
	assert.Contains(t, []string{"ollama", "local-slm"}, providerUsed)

	var fullText string
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		fullText += chunk.Text
	}
	assert.NotEmpty(t, fullText)
}

func TestProviderRouter_OpenAIPrimary(t *testing.T) {
	cfg := &config.ProvidersConfig{
		OpenAIAPIKey:   "sk-test-key",
		GeminiAPIKey:   "",
		TimeoutSeconds: 1,
	}

	router := NewProviderRouter(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, providerUsed, err := router.GenerateStream(ctx, "Test prompt")
	require.NoError(t, err)
	assert.Equal(t, "openai", providerUsed)

	var fullText string
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		fullText += chunk.Text
	}

	assert.Contains(t, fullText, "[OpenAI]")
}
