package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/cache"
	"github.com/theanhtong/rag_system/internal/config"
	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/router"
)

func TestChatHandler_HandleChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sc := cache.NewSemanticCache(rdb, &config.SemanticCacheConfig{
		SimilarityThreshold: 0.85,
		TTLSeconds:          3600,
	})
	pr := router.NewProviderRouter(&config.ProvidersConfig{OpenAIAPIKey: "sk-test-key"})
	emb := embedder.NewEmbeddingService(nil)
	chatHandler := NewChatHandler(nil, sc, pr, emb)

	engine := gin.New()
	engine.POST("/v1/chat/completions", chatHandler.HandleChatCompletions)

	// first request: expect cache MISS and provider fallback execution
	reqPayload := ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello world prompt"},
		},
		Stream: false,
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "MISS", w.Header().Get("X-Cache"))
	assert.Contains(t, w.Body.String(), "[OpenAI]")

	// wait briefly for asynchronous cache persistence goroutine
	time.Sleep(50 * time.Millisecond)

	// second request: expect cache HIT from Redis semantic cache
	req2, err := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBuffer(body))
	require.NoError(t, err)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	engine.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "HIT", w2.Header().Get("X-Cache"))
}
