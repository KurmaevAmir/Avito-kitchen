package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/service"
)

func (d Deps) listRestaurants(c *gin.Context) {
	limit, offset := parsePaging(c)
	filter := domain.OpenFilter(strings.TrimSpace(c.Query("is_open")))
	if filter == "" {
		filter = domain.OpenFilterOnlyOpen
	}
	items, err := d.Service.ListRestaurants(c.Request.Context(), filter, limit, offset)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gen.RestaurantListResponse{Items: toGenRestaurants(items)})
}

func (d Deps) getRestaurant(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	rest, err := d.Service.GetRestaurant(c.Request.Context(), id)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, toGenRestaurant(rest))
}

func (d Deps) getMenu(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	items, err := d.Service.GetMenu(c.Request.Context(), id)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gen.MenuListResponse{Items: toGenMenuItems(items)})
}

func (d Deps) createOrder(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	idem := c.GetHeader("Idempotency-Key")
	var body gen.CreateOrderRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	in := service.CreateOrderInput{
		UserID:          userID,
		RestaurantID:    body.RestaurantId,
		DeliveryAddress: body.DeliveryAddress,
		IdempotencyKey:  idem,
	}
	for _, it := range body.Items {
		in.Items = append(in.Items, struct {
			MenuItemID uuid.UUID
			Quantity   int
		}{it.MenuItemId, it.Quantity})
	}
	order, existed, err := d.Service.CreateOrder(c.Request.Context(), in)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	c.JSON(status, toGenOrder(order))
}

func (d Deps) listUserOrders(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	limit, offset := parsePaging(c)
	orders, err := d.Service.ListUserOrders(c.Request.Context(), userID, limit, offset)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gen.OrderListResponse{Items: toGenOrders(orders)})
}

func (d Deps) cancelOrder(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	var body gen.CancelOrderJSONBody
	if c.Request.ContentLength > 0 {
		if err := decodeJSON(c, &body); err != nil {
			writeError(c, d.Logger, domain.ErrValidation)
			return
		}
	}
	reason := ""
	if body.Reason != nil {
		reason = *body.Reason
	}
	order, err := d.Service.CancelOrder(c.Request.Context(), userID, id, reason)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(order))
}

func (d Deps) getOrderHistory(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	history, err := d.Service.GetOrderHistory(c.Request.Context(), userID, id)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toGenHistory(history)})
}

func (d Deps) getUserOrder(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	order, err := d.Service.GetUserOrder(c.Request.Context(), userID, id)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(order))
}
