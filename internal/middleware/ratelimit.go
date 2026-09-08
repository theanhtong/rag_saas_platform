package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/theanhtong/rag_system/internal/config"
)

// tokenBucketLuaScript performs atomic token & request rate limiting in Redis.
// Keys:
// 1. {client_key}:rpm
// 2. {client_key}:tpm
// ARGV:
// 1. max_rpm
// 2. max_tpm
// 3. estimated_tokens (or 1)
// 4. now_timestamp (seconds)
// 5. window_size (60 seconds)
// Returns: [allowed (0 or 1), remaining_rpm, remaining_tpm, reset_time_seconds]
const tokenBucketLuaScript = `
local rpm_key = KEYS[1]
local tpm_key = KEYS[2]

local max_rpm = tonumber(ARGV[1])
local max_tpm = tonumber(ARGV[2])
local cost_tokens = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local window = tonumber(ARGV[5])

local current_rpm = tonumber(redis.call('GET', rpm_key) or "0")
local current_tpm = tonumber(redis.call('GET', tpm_key) or "0")

if current_rpm + 1 > max_rpm or current_tpm + cost_tokens > max_tpm then
    local ttl = redis.call('TTL', rpm_key)
    if ttl < 0 then ttl = window end
    return {0, max_rpm - current_rpm, max_tpm - current_tpm, now + ttl}
end

local new_rpm = redis.call('INCRBY', rpm_key, 1)
if new_rpm == 1 then
    redis.call('EXPIRE', rpm_key, window)
end

local new_tpm = redis.call('INCRBY', tpm_key, cost_tokens)
if new_tpm == cost_tokens then
    redis.call('EXPIRE', tpm_key, window)
end

local ttl = redis.call('TTL', rpm_key)
if ttl < 0 then ttl = window end

return {1, max_rpm - new_rpm, max_tpm - new_tpm, now + ttl}
`

type RateLimiter struct {
	redisClient *redis.Client
	luaScript   *redis.Script
	cfg         *config.RateLimitConfig
}

func NewRateLimiter(rdb *redis.Client, cfg *config.RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		redisClient: rdb,
		luaScript:   redis.NewScript(tokenBucketLuaScript),
		cfg:         cfg,
	}
}

func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// extract tenant id or fallback to api key / client ip
		rateLimitKey := c.GetString("tenant_id")
		if rateLimitKey == "" {
			rateLimitKey = c.GetHeader("X-API-Key")
		}
		if rateLimitKey == "" {
			rateLimitKey = c.ClientIP()
		}

		// support dynamic per-tenant tier limits if set in gin context
		maxRPM := rl.cfg.RPM
		if customRPM := c.GetInt64("rate_limit_rpm"); customRPM > 0 {
			maxRPM = customRPM
		}

		maxTPM := rl.cfg.TPM
		if customTPM := c.GetInt64("rate_limit_tpm"); customTPM > 0 {
			maxTPM = customTPM
		}

		now := time.Now().Unix()
		estimatedTokens := int64(100) // default estimated prompt tokens per request

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		rpmKey := "ratelimit:" + rateLimitKey + ":rpm"
		tpmKey := "ratelimit:" + rateLimitKey + ":tpm"

		res, err := rl.luaScript.Run(ctx, rl.redisClient,
			[]string{rpmKey, tpmKey},
			maxRPM,
			maxTPM,
			estimatedTokens,
			now,
			60,
		).Result()

		if err != nil {
			// if Redis is temporarily down, log warning and allow request (fail open)
			c.Next()
			return
		}

		slice, ok := res.([]interface{})
		if !ok || len(slice) < 4 {
			c.Next()
			return
		}

		allowed := slice[0].(int64)
		remRPM := slice[1].(int64)
		remTPM := slice[2].(int64)
		resetSec := slice[3].(int64)

		if remRPM < 0 {
			remRPM = 0
		}
		if remTPM < 0 {
			remTPM = 0
		}

		c.Header("X-RateLimit-Limit-Requests", strconv.FormatInt(maxRPM, 10))
		c.Header("X-RateLimit-Remaining-Requests", strconv.FormatInt(remRPM, 10))
		c.Header("X-RateLimit-Limit-Tokens", strconv.FormatInt(maxTPM, 10))
		c.Header("X-RateLimit-Remaining-Tokens", strconv.FormatInt(remTPM, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetSec, 10))

		if allowed == 0 {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"code":    "rate_limit_exceeded",
					"message": "Token or request quota exceeded. Please try again later.",
				},
			})
			return
		}

		c.Next()
	}
}
