package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/config"
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
	tenantID := "tenant_acme"

	// 1. cache miss before set
	_, found, err := sc.Get(context.Background(), tenantID, promptVec)
	require.NoError(t, err)
	assert.False(t, found)

	// 2. set cache entry for tenant_acme
	err = sc.Set(context.Background(), tenantID, promptVec, promptText, respText)
	require.NoError(t, err)

	// 3. exact vector match for tenant_acme -> hit
	hit, found, err := sc.Get(context.Background(), tenantID, promptVec)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, respText, hit.Response)

	// 4. tenant isolation check -> tenant_stark should miss
	_, foundOther, err := sc.Get(context.Background(), "tenant_stark", promptVec)
	require.NoError(t, err)
	assert.False(t, foundOther)

	// 5. similar vector match for tenant_acme (similarity > 0.85) -> hit
	similarVec := []float32{0.51, 0.49, 0.5}
	hitSim, foundSim, err := sc.Get(context.Background(), tenantID, similarVec)
	require.NoError(t, err)
	assert.True(t, foundSim)
	assert.Equal(t, respText, hitSim.Response)

	// 6. dissimilar vector match -> miss
	dissimilarVec := []float32{-0.5, 0.5, 0.0}
	_, foundDis, err := sc.Get(context.Background(), tenantID, dissimilarVec)
	require.NoError(t, err)
	assert.False(t, foundDis)

	// 7. invalidate tenant cache
	err = sc.InvalidateTenantCache(context.Background(), tenantID)
	require.NoError(t, err)

	// 8. cache miss after invalidation
	_, foundAfterInvalidate, err := sc.Get(context.Background(), tenantID, promptVec)
	require.NoError(t, err)
	assert.False(t, foundAfterInvalidate)
}
