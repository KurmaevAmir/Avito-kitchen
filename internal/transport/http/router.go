// Package httpapi — HTTP-транспортный слой поверх Gin. Здесь роутинг,
// middleware, парсинг wire-формата и вызов сервисного слоя. Бизнес-
// валидация живёт в сервисе (см. ADR-10a в docs/architecture-plan.md).
//
// Типы request/response берутся из internal/api/gen, сгенерированного
// oapi-codegen'ом из api/openapi.yaml.
//
//go:generate oapi-codegen -config ../../api/oapi.yaml ../../../api/openapi.yaml
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
	"github.com/kurmaevamir/avito-kitchen/internal/service"
)

// Deps — зависимости транспортного слоя.
type Deps struct {
	Service          *service.Service
	Logger           *slog.Logger
	OpenAPIPath      string   // абсолютный путь к api/openapi.yaml для GET /openapi.yaml
	CORSOrigins      []string // разрешённые Origin'ы; nil = CORS выключен
	ValidateRequests bool     // включить kin-openapi request-validator middleware
	Pinger           interface {
		Ping(ctx context.Context) error
	}
}

// NewRouter собирает Gin-движок со всеми маршрутами и middleware.
func NewRouter(d Deps) http.Handler {
	r := gin.New()
	r.Use(gin.Recovery(), requestID(), requestLogger(d.Logger))
	if len(d.CORSOrigins) > 0 {
		r.Use(cors(d.CORSOrigins))
	}
	if d.ValidateRequests {
		mw, err := openapiValidator()
		if err != nil {
			d.Logger.Error("не удалось поднять OpenAPI validator", "err", err.Error())
		} else {
			r.Use(mw)
		}
	}

	r.GET("/healthz", d.healthz)
	r.GET("/readyz", d.readyz)
	r.GET("/openapi.yaml", d.serveOpenAPI)

	v1 := r.Group("/v1")
	{
		v1.GET("/restaurants", d.listRestaurants)
		v1.GET("/restaurants/:id", d.getRestaurant)
		v1.GET("/restaurants/:id/menu", d.getMenu)

		v1.POST("/orders", d.createOrder)
		v1.GET("/orders", d.listUserOrders)
		v1.GET("/orders/:id", d.getUserOrder)
		v1.POST("/orders/:id/cancel", d.cancelOrder)
		v1.GET("/orders/:id/history", d.getOrderHistory)

		partner := v1.Group("/partner", d.partnerAuth())
		{
			partner.GET("/menu", d.partnerListMenu)
			partner.POST("/menu", d.partnerAddMenu)
			partner.PATCH("/menu/:item_id", d.partnerPatchMenu)
			partner.DELETE("/menu/:item_id", d.partnerDeleteMenu)
			partner.GET("/orders", d.partnerListOrders)
			partner.PATCH("/orders/:id/status", d.partnerUpdateOrderStatus)
		}
	}

	return r
}

func (d Deps) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (d Deps) readyz(c *gin.Context) {
	if err := d.Pinger.Ping(c.Request.Context()); err != nil {
		// Не раскрываем детали (хост/порт/база в pg-ошибке) наружу.
		d.Logger.Warn("readiness ping не прошёл", "err", err.Error())
		c.AbortWithStatusJSON(http.StatusServiceUnavailable,
			gen.Error{Code: "not_ready", Message: "БД недоступна"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (d Deps) serveOpenAPI(c *gin.Context) {
	c.File(d.OpenAPIPath)
}
