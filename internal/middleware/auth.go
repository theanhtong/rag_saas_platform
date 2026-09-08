package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/theanhtong/rag_system/internal/service"
)

func AuthMiddleware(requiredAPIKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requiredAPIKey == "" {
			c.Next()
			return
		}

		apiKey := extractAPIKey(c)

		if apiKey != requiredAPIKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "unauthorized",
					"message": "Invalid or missing API key",
				},
			})
			return
		}

		c.Set("client_id", apiKey)
		c.Next()
	}
}

func MultiTenantAuthMiddleware(qs service.QuotaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := extractAPIKey(c)

		if qs == nil {
			if apiKey != "" {
				c.Set("client_id", apiKey)
				c.Set("key_hash", service.HashAPIKey(apiKey))
				c.Set("tenant_id", "tenant_default")
			}
			c.Next()
			return
		}

		details, err := qs.ValidateAPIKey(c.Request.Context(), apiKey)
		if err != nil {
			status := http.StatusUnauthorized
			code := "unauthorized"

			if errors.Is(err, service.ErrAPIKeyDisabled) {
				status = http.StatusForbidden
				code = "forbidden"
			} else if errors.Is(err, service.ErrQuotaExceeded) {
				status = http.StatusTooManyRequests
				code = "quota_exceeded"
			}

			c.AbortWithStatusJSON(status, gin.H{
				"error": gin.H{
					"code":    code,
					"message": err.Error(),
				},
			})
			return
		}

		c.Set("client_id", apiKey)
		c.Set("key_hash", details.KeyHash)
		c.Set("tenant_id", details.TenantID)
		c.Next()
	}
}

func extractAPIKey(c *gin.Context) string {
	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	return strings.TrimSpace(apiKey)
}
