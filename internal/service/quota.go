package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidAPIKey  = errors.New("invalid or missing API key")
	ErrAPIKeyDisabled = errors.New("API key has been revoked or disabled")
	ErrQuotaExceeded  = errors.New("monthly usage quota or token limit exceeded")
)

type APIKeyDetails struct {
	KeyHash          string  `json:"key_hash"`
	TenantID         string  `json:"tenant_id"`
	Name             string  `json:"name"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	UsedBudgetUSD    float64 `json:"used_budget_usd"`
	TokenLimit       int64   `json:"token_limit"`
	UsedTokens       int64   `json:"used_tokens"`
	IsActive         bool    `json:"is_active"`
}

type QuotaService interface {
	ValidateAPIKey(ctx context.Context, apiKey string) (*APIKeyDetails, error)
	RecordTokenUsage(ctx context.Context, keyHash, provider, model string, promptTokens, completionTokens int) error
	CalculateCost(provider, model string, promptTokens, completionTokens int) float64
}

type quotaService struct {
	rdb *redis.Client
}

func NewQuotaService(rdb *redis.Client) QuotaService {
	return &quotaService{rdb: rdb}
}

func HashAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

func (s *quotaService) ValidateAPIKey(ctx context.Context, apiKey string) (*APIKeyDetails, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}

	keyHash := HashAPIKey(apiKey)
	cacheKey := fmt.Sprintf("tenant_key:%s", keyHash)

	// check Redis cache first
	val, err := s.rdb.Get(ctx, cacheKey).Result()
	var details APIKeyDetails

	if err == nil && val != "" {
		if err := json.Unmarshal([]byte(val), &details); err == nil {
			return s.verifyQuota(&details)
		}
	}

	// fallback default key provisioning if not in Redis
	// standardized default testing key or custom user key
	details = APIKeyDetails{
		KeyHash:          keyHash,
		TenantID:         "tenant_default",
		Name:             "Active Key",
		MonthlyBudgetUSD: 50.00,
		UsedBudgetUSD:    0.00,
		TokenLimit:       5000000,
		UsedTokens:       0,
		IsActive:         true,
	}

	// fetch dynamic usage from Redis counters if present
	usedBudgetStr, _ := s.rdb.Get(ctx, fmt.Sprintf("usage_usd:%s", keyHash)).Result()
	if usedBudgetStr != "" {
		var usedUSD float64
		_, _ = fmt.Sscanf(usedBudgetStr, "%f", &usedUSD)
		details.UsedBudgetUSD = usedUSD
	}

	usedTokensStr, _ := s.rdb.Get(ctx, fmt.Sprintf("usage_tokens:%s", keyHash)).Result()
	if usedTokensStr != "" {
		var usedTokens int64
		_, _ = fmt.Sscanf(usedTokensStr, "%d", &usedTokens)
		details.UsedTokens = usedTokens
	}

	// cache key details for 5 minutes
	data, _ := json.Marshal(details)
	_ = s.rdb.Set(ctx, cacheKey, data, 5*time.Minute).Err()

	return s.verifyQuota(&details)
}

func (s *quotaService) verifyQuota(details *APIKeyDetails) (*APIKeyDetails, error) {
	if !details.IsActive {
		return nil, ErrAPIKeyDisabled
	}
	if details.MonthlyBudgetUSD > 0 && details.UsedBudgetUSD >= details.MonthlyBudgetUSD {
		return nil, ErrQuotaExceeded
	}
	if details.TokenLimit > 0 && details.UsedTokens >= details.TokenLimit {
		return nil, ErrQuotaExceeded
	}
	return details, nil
}

func (s *quotaService) RecordTokenUsage(ctx context.Context, keyHash, provider, model string, promptTokens, completionTokens int) error {
	if keyHash == "" {
		return nil
	}

	costUSD := s.CalculateCost(provider, model, promptTokens, completionTokens)
	totalTokens := int64(promptTokens + completionTokens)

	// increment Redis usage counters atomically
	usdKey := fmt.Sprintf("usage_usd:%s", keyHash)
	tokensKey := fmt.Sprintf("usage_tokens:%s", keyHash)

	if err := s.rdb.IncrByFloat(ctx, usdKey, costUSD).Err(); err != nil {
		log.Printf("failed to increment Redis USD usage: %v", err)
	}

	if err := s.rdb.IncrBy(ctx, tokensKey, totalTokens).Err(); err != nil {
		log.Printf("failed to increment Redis token usage: %v", err)
	}

	// update cached struct in Redis if present
	cacheKey := fmt.Sprintf("tenant_key:%s", keyHash)
	val, err := s.rdb.Get(ctx, cacheKey).Result()
	if err == nil && val != "" {
		var details APIKeyDetails
		if err := json.Unmarshal([]byte(val), &details); err == nil {
			details.UsedBudgetUSD += costUSD
			details.UsedTokens += totalTokens
			data, _ := json.Marshal(details)
			_ = s.rdb.Set(ctx, cacheKey, data, 5*time.Minute).Err()
		}
	}

	return nil
}

func (s *quotaService) CalculateCost(provider, model string, promptTokens, completionTokens int) float64 {
	model = strings.ToLower(model)

	var promptRate, completionRate float64

	switch {
	case strings.Contains(model, "gpt-4"):
		promptRate = 0.005 / 1000.0
		completionRate = 0.015 / 1000.0
	case strings.Contains(model, "gpt-3.5"):
		promptRate = 0.0005 / 1000.0
		completionRate = 0.0015 / 1000.0
	case strings.Contains(model, "gemini"):
		promptRate = 0.00015 / 1000.0
		completionRate = 0.0006 / 1000.0
	default:
		promptRate = 0.001 / 1000.0
		completionRate = 0.002 / 1000.0
	}

	return (float64(promptTokens) * promptRate) + (float64(completionTokens) * completionRate)
}
