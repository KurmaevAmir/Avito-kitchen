package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

// ctxRequestIDKey — приватный ключ, чтобы request_id из context.Context
// не пересекался с чужими значениями. Экспортируется только через
// RequestIDFromContext.
type ctxRequestIDKey struct{}

// RequestIDFromContext возвращает request_id, положенный middleware,
// или пустую строку, если контекст пришёл вне HTTP-запроса.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// requestID пробрасывает или генерирует X-Request-Id и кладёт значение
// одновременно в gin.Context и в стандартный context.Context — сервис и
// репозиторий смогут вытащить его через RequestIDFromContext.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Writer.Header().Set(requestIDHeader, rid)
		c.Set("request_id", rid)
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), ctxRequestIDKey{}, rid))
		c.Next()
	}
}

// requestLogger пишет одну структурную JSON-строку на каждый запрос.
func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		rid, _ := c.Get("request_id")
		log.Info("http-запрос",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"raw_path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"bytes", c.Writer.Size(),
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", c.ClientIP(),
			"request_id", rid,
		)
	}
}
