package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/llm_api_gateway/internal/config"
)

func TestCosineSimilarity(t *testing.T) {
	v1 := []float32{1.0, 0.0, 0.0}
	v2 := []float32{1.0, 0.0, 0.0}
	v3 := []float32{0.0, 1.0, 0.0}
	v4 := []float32{0.9, 0.1, 0.0}

	assert.InDelta(t, 1.0, CosineSimilarity(v1, v2), 0.0001)
	assert.InDelta(t, 0.0, CosineSimilarity(v1, v3), 0.0001)
	assert.Greater(t, CosineSimilarity(v1, v4), 0.9)
}

func TestSemanticCache_GetSet(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	cfg := &config.SemanticCacheConfig{
		SimilarityThreshold: 0.85,
		TTLSeconds:          3600,
	}

	sc := NewSemanticCache(rdb, cfg)

	promptVec := []float32{0.5, 0.5, 0.5}
	promptText := "What is the capital of France?"
	respText := "The capital of France is Paris."

	// 1. Cache Miss before Set
	_, found, err := sc.Get(context.Background(), promptVec)
	require.NoError(t, err)
	assert.False(t, found)

	// 2. Set Cache Entry
	err = sc.Set(context.Background(), promptVec, promptText, respText)
	require.NoError(t, err)

	// 3. Exact Vector Match -> Hit
	hit, found, err := sc.Get(context.Background(), promptVec)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, respText, hit.Response)

	// 4. Similar Vector Match (Similarity > 0.85) -> Hit
	similarVec := []float32{0.51, 0.49, 0.5}
	hitSim, foundSim, err := sc.Get(context.Background(), similarVec)
	require.NoError(t, err)
	assert.True(t, foundSim)
	assert.Equal(t, respText, hitSim.Response)

	// 5. Dissimilar Vector Match (Similarity < 0.85) -> Miss
	dissimilarVec := []float32{-0.5, 0.5, 0.0}
	_, foundDis, err := sc.Get(context.Background(), dissimilarVec)
	require.NoError(t, err)
	assert.False(t, foundDis)
}
