package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// writeError отображает доменные ошибки на HTTP-статусы и стабильные
// machine-readable code'ы. Поле code — снейк-кейс латиницей, message —
// человекочитаемый русский текст.
func writeError(c *gin.Context, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		// 401 требует WWW-Authenticate по RFC 7235.
		c.Writer.Header().Set("WWW-Authenticate", `Bearer realm="partner"`)
		abort(c, http.StatusUnauthorized, "unauthorized", err)

	case errors.Is(err, domain.ErrNotFound):
		abort(c, http.StatusNotFound, "not_found", err)

	case errors.Is(err, domain.ErrIdempotencyMismatch):
		abort(c, http.StatusConflict, "idempotency_key_reuse", err)

	case errors.Is(err, domain.ErrForbiddenTransition):
		abort(c, http.StatusConflict, "forbidden_transition", err)
	case errors.Is(err, domain.ErrItemUnavailable):
		abort(c, http.StatusConflict, "item_unavailable", err)
	case errors.Is(err, domain.ErrRestaurantClosed):
		abort(c, http.StatusConflict, "restaurant_closed", err)
	case errors.Is(err, domain.ErrMismatchedRestaurant):
		// Клиентская ошибка (позиции из разных заведений) — 400, а не 409.
		abort(c, http.StatusBadRequest, "mismatched_restaurant", err)
	case errors.Is(err, domain.ErrConflict):
		abort(c, http.StatusConflict, "conflict", err)

	case errors.Is(err, domain.ErrValidation):
		abort(c, http.StatusBadRequest, "validation_failed", err)

	default:
		log.Error("необработанная ошибка", "err", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError,
			gen.Error{Code: "internal_error", Message: "внутренняя ошибка сервера"})
	}
}

func abort(c *gin.Context, status int, code string, err error) {
	c.AbortWithStatusJSON(status, gen.Error{Code: code, Message: err.Error()})
}

// maxRequestBytes ограничивает тело запроса — заказ на 100k позиций
// не должен превратить один POST в 100k INSERT'ов.
const maxRequestBytes = 128 * 1024 // 128 KiB

// decodeJSON декодирует тело запроса строго: неизвестные поля отвергаются
// (защита от опечаток), тело ограничено MaxBytesReader (защита от DoS).
func decodeJSON(c *gin.Context, dst any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
