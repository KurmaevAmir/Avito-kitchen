package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

const restaurantCtxKey = "restaurant"

// bearerToken извлекает токен из заголовка Authorization: Bearer <token>.
// Пустая строка сигнализирует "не передан" и пойдёт в сервис, как
// ErrValidation.
func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// partnerAuth валидирует Authorization: Bearer <api_key> и кладёт
// найденное заведение в контекст запроса под ключом restaurantCtxKey.
func (d Deps) partnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		rest, err := d.Service.AuthenticatePartner(c.Request.Context(), token)
		if err != nil {
			writeError(c, d.Logger, err)
			return
		}
		c.Set(restaurantCtxKey, rest)
		c.Next()
	}
}

// restaurantFrom извлекает заведение, положенное partnerAuth.
func restaurantFrom(c *gin.Context) domain.Restaurant {
	v, _ := c.Get(restaurantCtxKey)
	r, _ := v.(domain.Restaurant)
	return r
}

func (d Deps) partnerListMenu(c *gin.Context) {
	rest := restaurantFrom(c)
	items, err := d.Service.PartnerMenu(c.Request.Context(), rest.ID)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gen.MenuListResponse{Items: toGenMenuItems(items)})
}

func (d Deps) partnerAddMenu(c *gin.Context) {
	rest := restaurantFrom(c)
	var body gen.UpsertMenuItemRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	available := true
	if body.Available != nil {
		available = *body.Available
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	composition := ""
	if body.Composition != nil {
		composition = *body.Composition
	}
	var allergens []string
	if body.Allergens != nil {
		allergens = *body.Allergens
	} else {
		allergens = []string{}
	}
	item, err := d.Service.AddMenuItem(c.Request.Context(), domain.MenuItem{
		RestaurantID: rest.ID,
		Name:         body.Name,
		Description:  description,
		Price:        body.Price,
		WeightGrams:  body.WeightGrams,
		Composition:  composition,
		Allergens:    allergens,
		Available:    available,
	})
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusCreated, toGenMenuItem(item))
}

func (d Deps) partnerPatchMenu(c *gin.Context) {
	rest := restaurantFrom(c)
	itemID, err := uuid.Parse(c.Param("item_id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	var body gen.PatchMenuItemRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	item, err := d.Service.PatchMenuItem(c.Request.Context(), rest.ID, itemID, body.Price, body.Available)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, toGenMenuItem(item))
}

func (d Deps) partnerDeleteMenu(c *gin.Context) {
	rest := restaurantFrom(c)
	itemID, err := uuid.Parse(c.Param("item_id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	if err := d.Service.DeleteMenuItem(c.Request.Context(), rest.ID, itemID); err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (d Deps) partnerListOrders(c *gin.Context) {
	rest := restaurantFrom(c)
	status := c.Query("status")
	limit, _ := parsePaging(c)
	orders, err := d.Service.ListRestaurantOrders(c.Request.Context(), rest.ID, status, limit)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, gen.OrderListResponse{Items: toGenOrders(orders)})
}

func (d Deps) partnerUpdateOrderStatus(c *gin.Context) {
	rest := restaurantFrom(c)
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	var body gen.UpdateOrderStatusRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, d.Logger, domain.ErrValidation)
		return
	}
	comment := ""
	if body.Comment != nil {
		comment = *body.Comment
	}
	order, err := d.Service.UpdateOrderStatus(c.Request.Context(), rest.ID, orderID,
		domain.OrderStatus(body.Status), comment)
	if err != nil {
		writeError(c, d.Logger, err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(order))
}
