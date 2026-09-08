package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Expose-Headers: X-Request-Id — чтобы клиент мог показать ID
// в интерфейсе поддержки/логах бага.
func cors(allowed []string) gin.HandlerFunc {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := set[origin]; ok {
				h := c.Writer.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Headers",
					"Content-Type, Authorization, X-User-Id, Idempotency-Key")
				h.Set("Access-Control-Allow-Methods",
					"GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Expose-Headers", "X-Request-Id")
				h.Set("Access-Control-Max-Age", "600")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
