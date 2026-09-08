package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
)

// repoStub — минимальный мок Repository для unit-тестов сервиса.
// Все методы возвращают заранее заданные значения; счётчики позволяют
// проверить факт вызова.
type repoStub struct {
	pingErr           error
	restaurant        domain.Restaurant
	restaurantErr     error
	restaurantByHash  domain.Restaurant
	restaurantHashErr error

	createOrderFn       func(ctx context.Context, in repository.CreateOrderInput) (domain.Order, bool, error)
	updateStatusFn      func(ctx context.Context, restaurantID, orderID uuid.UUID, next domain.OrderStatus, comment string) (domain.Order, bool, error)
	softDeleteMenuFn    func(ctx context.Context, restaurantID, itemID uuid.UUID) error
	listAvailableMenuFn func(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error)
	createMenuItemFn    func(ctx context.Context, m domain.MenuItem) (domain.MenuItem, error)
	getOrderFn          func(ctx context.Context, id uuid.UUID) (domain.Order, error)
	listOrderHistoryFn  func(ctx context.Context, id uuid.UUID) ([]domain.OrderStatusEvent, error)

	callsCreateOrder  int
	callsUpdateStatus int
	callsSoftDelete   int
}

func (s *repoStub) Ping(context.Context) error { return s.pingErr }

func (s *repoStub) ListRestaurants(context.Context, domain.OpenFilter, int, int) ([]domain.Restaurant, error) {
	return []domain.Restaurant{s.restaurant}, nil
}
func (s *repoStub) GetRestaurant(context.Context, uuid.UUID) (domain.Restaurant, error) {
	return s.restaurant, s.restaurantErr
}
func (s *repoStub) GetRestaurantByAPIKeyHash(context.Context, string) (domain.Restaurant, error) {
	return s.restaurantByHash, s.restaurantHashErr
}
func (s *repoStub) ListAvailableMenu(ctx context.Context, id uuid.UUID) ([]domain.MenuItem, error) {
	if s.listAvailableMenuFn != nil {
		return s.listAvailableMenuFn(ctx, id)
	}
	return nil, nil
}
func (s *repoStub) ListAllMenu(context.Context, uuid.UUID) ([]domain.MenuItem, error) {
	return nil, nil
}
func (s *repoStub) CreateMenuItem(ctx context.Context, m domain.MenuItem) (domain.MenuItem, error) {
	if s.createMenuItemFn != nil {
		return s.createMenuItemFn(ctx, m)
	}
	m.ID = uuid.New()
	m.CreatedAt = time.Now()
	return m, nil
}
func (s *repoStub) PatchMenuItem(context.Context, uuid.UUID, uuid.UUID, *int64, *bool) (domain.MenuItem, error) {
	return domain.MenuItem{}, nil
}
func (s *repoStub) SoftDeleteMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID) error {
	s.callsSoftDelete++
	if s.softDeleteMenuFn != nil {
		return s.softDeleteMenuFn(ctx, restaurantID, itemID)
	}
	return nil
}
func (s *repoStub) CreateOrder(ctx context.Context, in repository.CreateOrderInput) (domain.Order, bool, error) {
	s.callsCreateOrder++
	if s.createOrderFn != nil {
		return s.createOrderFn(ctx, in)
	}
	return domain.Order{
		ID: uuid.New(), UserID: in.UserID, RestaurantID: in.RestaurantID,
		Status: domain.OrderStatusCreated,
	}, false, nil
}
func (s *repoStub) GetOrder(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	if s.getOrderFn != nil {
		return s.getOrderFn(ctx, id)
	}
	return domain.Order{ID: id}, nil
}
func (s *repoStub) ListOrderStatusHistory(ctx context.Context, id uuid.UUID) ([]domain.OrderStatusEvent, error) {
	if s.listOrderHistoryFn != nil {
		return s.listOrderHistoryFn(ctx, id)
	}
	return nil, nil
}
func (s *repoStub) ListUserOrders(context.Context, string, int, int) ([]domain.Order, error) {
	return nil, nil
}
func (s *repoStub) ListRestaurantOrders(context.Context, uuid.UUID, string, int) ([]domain.Order, error) {
	return nil, nil
}
func (s *repoStub) UpdateOrderStatus(ctx context.Context, restaurantID, orderID uuid.UUID, next domain.OrderStatus, comment string) (domain.Order, bool, error) {
	s.callsUpdateStatus++
	if s.updateStatusFn != nil {
		return s.updateStatusFn(ctx, restaurantID, orderID, next, comment)
	}
	return domain.Order{ID: orderID, RestaurantID: restaurantID, Status: next}, true, nil
}

// pubStub — счётчик публикаций для проверки, что событие ушло/не ушло.
type pubStub struct {
	createdCount       int
	statusChangedCount int
	lastStatusComment  string
	lastCreatedOrderID uuid.UUID
}

func (p *pubStub) PublishOrderCreated(_ context.Context, o domain.Order) {
	p.createdCount++
	p.lastCreatedOrderID = o.ID
}
func (p *pubStub) PublishOrderStatusChanged(_ context.Context, _ domain.Order, comment string) {
	p.statusChangedCount++
	p.lastStatusComment = comment
}
func (p *pubStub) Close() error { return nil }
