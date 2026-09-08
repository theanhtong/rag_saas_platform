package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaService_ValidateAndRecordUsage(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	qs := NewQuotaService(rdb)

	ctx := context.Background()

	// case 1: empty key -> invalid
	_, err = qs.ValidateAPIKey(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidAPIKey)

	// case 2: valid key -> passes
	apiKey := "test_api_key_123"
	details, err := qs.ValidateAPIKey(ctx, apiKey)
	require.NoError(t, err)
	assert.Equal(t, "tenant_default", details.TenantID)
	assert.True(t, details.IsActive)

	// case 3: record token usage
	keyHash := HashAPIKey(apiKey)
	err = qs.RecordTokenUsage(ctx, keyHash, "openai", "gpt-4o", 1000, 2000)
	require.NoError(t, err)

	// case 4: cost calculation
	cost := qs.CalculateCost("openai", "gpt-4o", 1000, 2000)
	assert.Greater(t, cost, 0.0)
}

func TestQuotaService_ExceededLimit(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	qs := NewQuotaService(rdb)
	ctx := context.Background()

	apiKey := "limited_key"
	keyHash := HashAPIKey(apiKey)

	_ = mr.Set("usage_usd:"+keyHash, "60.00")

	_, err = qs.ValidateAPIKey(ctx, apiKey)
	assert.ErrorIs(t, err, ErrQuotaExceeded)
}
