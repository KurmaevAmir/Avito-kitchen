// Package domain содержит доменные сущности и правила Авито.Кухни.
// Не зависит ни от одного слоя инфраструктуры (БД, HTTP, шина).
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrderStatus — стадия жизненного цикла заказа. Значения совпадают со
// строками enum-типа order_status в PostgreSQL.
type OrderStatus string

// Возможные значения OrderStatus.
const (
	OrderStatusCreated   OrderStatus = "created"
	OrderStatusAccepted  OrderStatus = "accepted"
	OrderStatusCooking   OrderStatus = "cooking"
	OrderStatusReady     OrderStatus = "ready"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// Valid сообщает, соответствует ли значение одному из известных статусов.
func (s OrderStatus) Valid() bool {
	switch s {
	case OrderStatusCreated, OrderStatusAccepted, OrderStatusCooking,
		OrderStatusReady, OrderStatusDelivered, OrderStatusCancelled:
		return true
	}
	return false
}

// CanTransitionTo проверяет допустимость перехода из текущего статуса в
// целевой. Терминальные состояния (delivered, cancelled) переходов не
// имеют.
func (s OrderStatus) CanTransitionTo(next OrderStatus) bool {
	allowed := map[OrderStatus][]OrderStatus{
		OrderStatusCreated:   {OrderStatusAccepted, OrderStatusCancelled},
		OrderStatusAccepted:  {OrderStatusCooking, OrderStatusCancelled},
		OrderStatusCooking:   {OrderStatusReady, OrderStatusCancelled},
		OrderStatusReady:     {OrderStatusDelivered},
		OrderStatusDelivered: {},
		OrderStatusCancelled: {},
	}
	for _, a := range allowed[s] {
		if a == next {
			return true
		}
	}
	return false
}

// OpenFilter — значение query-параметра ?is_open для GET /v1/restaurants.
type OpenFilter string

// Возможные значения OpenFilter.
const (
	OpenFilterOnlyOpen   OpenFilter = "true" // по умолчанию
	OpenFilterOnlyClosed OpenFilter = "false"
	OpenFilterAll        OpenFilter = "all"
)

// Valid сообщает, соответствует ли значение одному из известных фильтров.
func (f OpenFilter) Valid() bool {
	switch f {
	case OpenFilterOnlyOpen, OpenFilterOnlyClosed, OpenFilterAll:
		return true
	}
	return false
}

// Restaurant — заведение, размещённое в Авито.Кухне.
type Restaurant struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Address     string    `json:"address"`
	IsOpen      bool      `json:"is_open"`
	CreatedAt   time.Time `json:"created_at"`
}

// MenuItem — позиция меню конкретного заведения.
type MenuItem struct {
	ID           uuid.UUID `json:"id"`
	RestaurantID uuid.UUID `json:"restaurant_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Price        int64     `json:"price"` // копейки
	WeightGrams  *int      `json:"weight_grams,omitempty"`
	Composition  string    `json:"composition"`
	Allergens    []string  `json:"allergens"`
	Available    bool      `json:"available"`
	CreatedAt    time.Time `json:"created_at"`
}

// Order — заказ пользователя. Идемпотентность на уровне БД обеспечивается
// уникальностью пары (UserID, IdempotencyKey).
type Order struct {
	ID             uuid.UUID   `json:"id"`
	UserID         string      `json:"user_id"`
	RestaurantID   uuid.UUID   `json:"restaurant_id"`
	Status         OrderStatus `json:"status"`
	TotalPrice     int64       `json:"total_price"` // копейки
	DeliveryAddr   string      `json:"delivery_address"`
	IdempotencyKey string      `json:"-"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Items          []OrderItem `json:"items"`
}

// OrderItem — строка заказа со снимком имени и цены позиции меню на
// момент создания заказа: изменение меню не переписывает историю.
type OrderItem struct {
	ID         uuid.UUID `json:"id"`
	OrderID    uuid.UUID `json:"order_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Name       string    `json:"name"`
	Price      int64     `json:"price"` // копейки, snapshot
	Quantity   int       `json:"quantity"`
}

// OrderStatusEvent — запись из истории смен статуса заказа.
type OrderStatusEvent struct {
	OrderID   uuid.UUID   `json:"order_id"`
	Status    OrderStatus `json:"status"`
	ChangedAt time.Time   `json:"changed_at"`
	Comment   string      `json:"comment,omitempty"`
}

// Доменные ошибки. Транспортный слой преобразует их в HTTP-статусы
// и в стабильные machine-readable code'ы (см. writeError).
var (
	ErrNotFound             = errors.New("ресурс не найден")
	ErrValidation           = errors.New("ошибка валидации")
	ErrUnauthorized         = errors.New("требуется аутентификация")
	ErrConflict             = errors.New("конфликт состояния")
	ErrForbiddenTransition  = errors.New("недопустимый переход статуса")
	ErrItemUnavailable      = errors.New("позиция меню недоступна")
	ErrRestaurantClosed     = errors.New("заведение закрыто")
	ErrMismatchedRestaurant = errors.New("позиции принадлежат разным заведениям")
	ErrIdempotencyMismatch  = errors.New("Idempotency-Key переиспользован с другим телом запроса")
)
