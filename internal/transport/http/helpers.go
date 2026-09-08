package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parsePaging извлекает limit/offset из query. Верхний предел limit
func parsePaging(c *gin.Context) (limit, offset int) {
	limit, offset = 50, 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}
