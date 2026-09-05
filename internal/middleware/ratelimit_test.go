package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/llm_api_gateway/internal/config"
)

func TestRateLimiter_Middleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	cfg := &config.RateLimitConfig{
		RPM: 2,    // Allow 2 requests per minute
		TPM: 1000, // Allow 1000 tokens per minute
	}

	limiter := NewRateLimiter(rdb, cfg)

	router := gin.New()
	router.Use(limiter.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Request 1: Should pass (200)
	req1, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req1.Header.Set("X-API-Key", "test-key")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, "2", w1.Header().Get("X-RateLimit-Limit-Requests"))
	assert.Equal(t, "1", w1.Header().Get("X-RateLimit-Remaining-Requests"))

	// Request 2: Should pass (200)
	req2, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("X-API-Key", "test-key")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "0", w2.Header().Get("X-RateLimit-Remaining-Requests"))

	// Request 3: Exceeds RPM limit -> Should return 429 Too Many Requests
	req3, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req3.Header.Set("X-API-Key", "test-key")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	assert.Equal(t, http.StatusTooManyRequests, w3.Code)
	assert.Contains(t, w3.Body.String(), "rate_limit_exceeded")
}
