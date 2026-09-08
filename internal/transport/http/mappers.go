package httpapi

import (
	"time"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// Мапперы domain в gen. Границу "внутренние модели ≠ wire-формат"
// держим явной: DTO — это источник правды для wire, domain — для бизнес-
// логики. Позволяет менять представление одного, не трогая другое.

func toGenRestaurant(r domain.Restaurant) gen.Restaurant {
	desc := r.Description
	return gen.Restaurant{
		Id:          r.ID,
		Name:        r.Name,
		Description: &desc,
		Address:     r.Address,
		IsOpen:      r.IsOpen,
		CreatedAt:   r.CreatedAt,
	}
}

func toGenRestaurants(in []domain.Restaurant) []gen.Restaurant {
	out := make([]gen.Restaurant, 0, len(in))
	for _, r := range in {
		out = append(out, toGenRestaurant(r))
	}
	return out
}

func toGenMenuItem(m domain.MenuItem) gen.MenuItem {
	desc := m.Description
	comp := m.Composition
	// Ненулевой slice гарантирует JSON `allergens: []` вместо null.
	allergens := m.Allergens
	if allergens == nil {
		allergens = []string{}
	}
	return gen.MenuItem{
		Id:           m.ID,
		RestaurantId: m.RestaurantID,
		Name:         m.Name,
		Description:  &desc,
		Price:        m.Price,
		WeightGrams:  m.WeightGrams,
		Composition:  &comp,
		Allergens:    &allergens,
		Available:    m.Available,
		CreatedAt:    m.CreatedAt,
	}
}

func toGenMenuItems(in []domain.MenuItem) []gen.MenuItem {
	out := make([]gen.MenuItem, 0, len(in))
	for _, m := range in {
		out = append(out, toGenMenuItem(m))
	}
	return out
}

func toGenOrder(o domain.Order) gen.Order {
	items := make([]gen.OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, gen.OrderItem{
			Id:         it.ID,
			MenuItemId: it.MenuItemID,
			Name:       it.Name,
			Price:      it.Price,
			Quantity:   it.Quantity,
		})
	}
	return gen.Order{
		Id:              o.ID,
		UserId:          o.UserID,
		RestaurantId:    o.RestaurantID,
		Status:          gen.OrderStatus(o.Status),
		TotalPrice:      o.TotalPrice,
		DeliveryAddress: o.DeliveryAddr,
		CreatedAt:       o.CreatedAt,
		UpdatedAt:       o.UpdatedAt,
		Items:           items,
	}
}

// historyItem является inline-DTO для /orders/{id}/history. Спека описывает
// его inline, oapi-codegen не создаёт для inline-arrays именованный тип.
type historyItem struct {
	Status    string    `json:"status"`
	Comment   string    `json:"comment,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

func toGenHistory(in []domain.OrderStatusEvent) []historyItem {
	out := make([]historyItem, 0, len(in))
	for _, e := range in {
		out = append(out, historyItem{
			Status:    string(e.Status),
			Comment:   e.Comment,
			ChangedAt: e.ChangedAt,
		})
	}
	return out
}

func toGenOrders(in []domain.Order) []gen.Order {
	out := make([]gen.Order, 0, len(in))
	for _, o := range in {
		out = append(out, toGenOrder(o))
	}
	return out
}
