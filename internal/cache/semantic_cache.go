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
	Get(ctx context.Context, vector []float32) (*CachedResponse, bool, error)
	Set(ctx context.Context, vector []float32, prompt string, response string) error
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

func (c *redisSemanticCache) Get(ctx context.Context, vector []float32) (*CachedResponse, bool, error) {
	if len(vector) == 0 {
		return nil, false, nil
	}

	keys, err := c.client.Keys(ctx, "semantic_cache:*").Result()
	if err != nil || len(keys) == 0 {
		return nil, false, nil
	}

	var bestMatch *CachedResponse
	var maxSimilarity float64 = 0.0

	// Scan entries to compute Cosine Similarity
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

func (c *redisSemanticCache) Set(ctx context.Context, vector []float32, prompt string, response string) error {
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

	key := fmt.Sprintf("semantic_cache:%d", time.Now().UnixNano())
	ttl := time.Duration(c.cfg.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}

	return c.client.Set(ctx, key, data, ttl).Err()
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
