package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/config"
)

type CachedResponse struct {
	Prompt     string    `json:"prompt"`
	Response   string    `json:"response"`
	Vector     []float32 `json:"vector"`
	Similarity float64   `json:"similarity,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type SemanticCache interface {
	Get(ctx context.Context, tenantID string, vector []float32) (*CachedResponse, bool, error)
	Set(ctx context.Context, tenantID string, vector []float32, prompt string, response string) error
	InvalidateTenantCache(ctx context.Context, tenantID string) error
}

type redisSemanticCache struct {
	client *redis.Client
	cfg    *config.SemanticCacheConfig
}

func NewSemanticCache(rdb *redis.Client, cfg *config.SemanticCacheConfig) SemanticCache {
	return &redisSemanticCache{
		client: rdb,
		cfg:    cfg,
	}
}

func (c *redisSemanticCache) Get(ctx context.Context, tenantID string, vector []float32) (*CachedResponse, bool, error) {
	if len(vector) == 0 {
		return nil, false, nil
	}

	if tenantID == "" {
		tenantID = "global"
	}

	matchPattern := fmt.Sprintf("semantic_cache:%s:*", tenantID)
	var keys []string
	var cursor uint64

	// use non-blocking SCAN instead of KEYS for production Redis scaling
	for {
		resKeys, nextCursor, err := c.client.Scan(ctx, cursor, matchPattern, 100).Result()
		if err != nil {
			break
		}
		keys = append(keys, resKeys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(keys) == 0 {
		return nil, false, nil
	}

	var bestMatch *CachedResponse
	var maxSimilarity float64 = 0.0

	// scan matching tenant entries to compute CosineSimilarity
	for _, key := range keys {
		val, err := c.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var entry CachedResponse
		if err := json.Unmarshal([]byte(val), &entry); err != nil {
			continue
		}

		sim := CosineSimilarity(vector, entry.Vector)
		if sim > maxSimilarity {
			maxSimilarity = sim
			bestMatch = &entry
			bestMatch.Similarity = sim
		}
	}

	if maxSimilarity >= c.cfg.SimilarityThreshold && bestMatch != nil {
		return bestMatch, true, nil
	}

	return nil, false, nil
}

func (c *redisSemanticCache) Set(ctx context.Context, tenantID string, vector []float32, prompt string, response string) error {
	if tenantID == "" {
		tenantID = "global"
	}

	entry := CachedResponse{
		Prompt:    prompt,
		Response:  response,
		Vector:    vector,
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	key := fmt.Sprintf("semantic_cache:%s:%d", tenantID, time.Now().UnixNano())
	ttl := time.Duration(c.cfg.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}

	return c.client.Set(ctx, key, data, ttl).Err()
}

func (c *redisSemanticCache) InvalidateTenantCache(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		tenantID = "global"
	}

	matchPattern := fmt.Sprintf("semantic_cache:%s:*", tenantID)
	var cursor uint64

	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, matchPattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys for invalidation: %w", err)
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("failed to delete tenant cache keys: %w", err)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// CosineSimilarity calculates the dot product divided by magnitude product.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct float64 = 0.0
	var normA float64 = 0.0
	var normB float64 = 0.0

	for i := 0; i < len(a); i++ {
		valA := float64(a[i])
		valB := float64(b[i])
		dotProduct += valA * valB
		normA += valA * valA
		normB += valB * valB
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
