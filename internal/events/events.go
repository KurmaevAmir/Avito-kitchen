// Package events содержит wire-контракт бизнес-событий Авито.Кухни и
// реализации publisher'а: RabbitMQ и no-op. Абстракция
// (service.Publisher) живёт у потребителя.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// ExchangeOrders — имя topic-exchange в RabbitMQ, куда публикуются
// все события заказов.
//
// Routing-keys мультитенантные — включают id заведения, чтобы очередь
// одного партнёра не получала события другого:
//
//	orders.<restaurant_id>.created
//	orders.<restaurant_id>.status.<new_status>
//
// Подписчик биндит очередь по паттерну orders.<own_id>.# и получает
// только свой поток.
const ExchangeOrders = "avito.kitchen.orders"

// RoutingCreatedFor и RoutingStatusFor собирают routing key для
// конкретного заведения. Держим их в одном месте, чтобы publisher и
// consumer не разъехались.
func RoutingCreatedFor(restaurantID string) string {
	return "orders." + restaurantID + ".created"
}

// RoutingStatusFor — routing-key события смены статуса конкретного заведения.
func RoutingStatusFor(restaurantID, status string) string {
	return "orders." + restaurantID + ".status." + status
}

// OrderCreatedEvent — payload события создания заказа. Стабильный wire-
// контракт: имена и типы полей менять только с версионированием.
type OrderCreatedEvent struct {
	EventID      uuid.UUID   `json:"event_id"`
	OccurredAt   time.Time   `json:"occurred_at"`
	OrderID      uuid.UUID   `json:"order_id"`
	RestaurantID uuid.UUID   `json:"restaurant_id"`
	UserID       string      `json:"user_id"`
	TotalPrice   int64       `json:"total_price"` // копейки
	Items        []OrderItem `json:"items"`
}

// OrderStatusChangedEvent — payload события смены статуса заказа.
type OrderStatusChangedEvent struct {
	EventID      uuid.UUID          `json:"event_id"`
	OccurredAt   time.Time          `json:"occurred_at"`
	OrderID      uuid.UUID          `json:"order_id"`
	RestaurantID uuid.UUID          `json:"restaurant_id"`
	NewStatus    domain.OrderStatus `json:"new_status"`
	Comment      string             `json:"comment,omitempty"`
}

// OrderItem — упрощённая позиция заказа для payload события.
type OrderItem struct {
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Name       string    `json:"name"`
	Price      int64     `json:"price"` // копейки
	Quantity   int       `json:"quantity"`
}

// NoopPublisher — реализация-заглушка: методы Publish* ничего не делают.
type NoopPublisher struct{}

// NewNoopPublisher возвращает заглушку publisher'а.
func NewNoopPublisher() NoopPublisher { return NoopPublisher{} }

// PublishOrderCreated — no-op.
func (NoopPublisher) PublishOrderCreated(context.Context, domain.Order) {}

// PublishOrderStatusChanged — no-op.
func (NoopPublisher) PublishOrderStatusChanged(context.Context, domain.Order, string) {}

// Close — no-op.
func (NoopPublisher) Close() error { return nil }

// eventItems конвертирует доменные позиции в payload-формат события.
func eventItems(in []domain.OrderItem) []OrderItem {
	out := make([]OrderItem, 0, len(in))
	for _, it := range in {
		out = append(out, OrderItem{
			MenuItemID: it.MenuItemID,
			Name:       it.Name,
			Price:      it.Price,
			Quantity:   it.Quantity,
		})
	}
	return out
}
