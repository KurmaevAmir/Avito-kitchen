package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
)

// Repository — контракт хранилища доменных объектов, требуемый сервису.
// Реализация — internal/repository (pgx поверх PostgreSQL); в тестах
// подставляется мок.
type Repository interface {
	Ping(ctx context.Context) error

	ListRestaurants(ctx context.Context, filter domain.OpenFilter, limit, offset int) ([]domain.Restaurant, error)
	GetRestaurant(ctx context.Context, id uuid.UUID) (domain.Restaurant, error)
	GetRestaurantByAPIKeyHash(ctx context.Context, hash string) (domain.Restaurant, error)

	ListAvailableMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error)
	ListAllMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error)
	CreateMenuItem(ctx context.Context, m domain.MenuItem) (domain.MenuItem, error)
	PatchMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID, price *int64, available *bool) (domain.MenuItem, error)
	SoftDeleteMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID) error

	CreateOrder(ctx context.Context, in repository.CreateOrderInput) (domain.Order, bool, error)
	GetOrder(ctx context.Context, id uuid.UUID) (domain.Order, error)
	ListOrderStatusHistory(ctx context.Context, orderID uuid.UUID) ([]domain.OrderStatusEvent, error)
	ListUserOrders(ctx context.Context, userID string, limit, offset int) ([]domain.Order, error)
	ListRestaurantOrders(ctx context.Context, restaurantID uuid.UUID, status string, limit int) ([]domain.Order, error)
	UpdateOrderStatus(ctx context.Context, restaurantID, orderID uuid.UUID, next domain.OrderStatus, comment string) (order domain.Order, changed bool, err error)
}

// Cache — контракт read-through кэша с JSON-сериализацией.
// Реализация должна быть fail-open: сбои Get/Set/Del не поднимаются
// в вызывающий код (иначе недоступный Redis рушит основной запрос).
type Cache interface {
	Get(ctx context.Context, key string, dst any) (hit bool, err error)
	Set(ctx context.Context, key string, val any, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Close() error
}

// Publisher — контракт публикации бизнес-событий. Возврата ошибки нет:
// шина не должна ронять основной путь; сбои внутри реализации
// логгируются самой реализацией.
type Publisher interface {
	PublishOrderCreated(ctx context.Context, order domain.Order)
	PublishOrderStatusChanged(ctx context.Context, order domain.Order, comment string)
	Close() error
}
