package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
)

// CreateOrderInput — входные данные создания заказа.
type CreateOrderInput struct {
	UserID          string
	RestaurantID    uuid.UUID
	DeliveryAddress string
	IdempotencyKey  string
	Items           []struct {
		MenuItemID uuid.UUID
		Quantity   int
	}
}

// CreateOrder создаёт заказ или возвращает существующий при идемпотентном
// повторе. Второй возвращаемый параметр — признак повтора. При реальном
// создании публикует событие orders.created; на идемпотентном повторе
// событие не публикуется — иначе подписчики получили бы дубль.
func (s *Service) CreateOrder(ctx context.Context, in CreateOrderInput) (domain.Order, bool, error) {
	if strings.TrimSpace(in.UserID) == "" {
		return domain.Order{}, false, fmt.Errorf("%w: не передан заголовок X-User-Id", domain.ErrValidation)
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return domain.Order{}, false, fmt.Errorf("%w: не передан заголовок Idempotency-Key", domain.ErrValidation)
	}
	if strings.TrimSpace(in.DeliveryAddress) == "" {
		return domain.Order{}, false, fmt.Errorf("%w: не указан адрес доставки", domain.ErrValidation)
	}
	if len(in.Items) == 0 {
		return domain.Order{}, false, fmt.Errorf("%w: не переданы позиции заказа", domain.ErrValidation)
	}
	repoItems := make([]repository.CreateOrderItem, 0, len(in.Items))
	for _, it := range in.Items {
		if it.Quantity <= 0 {
			return domain.Order{}, false, fmt.Errorf("%w: количество должно быть положительным", domain.ErrValidation)
		}
		repoItems = append(repoItems, repository.CreateOrderItem{
			MenuItemID: it.MenuItemID,
			Quantity:   it.Quantity,
		})
	}
	order, existed, err := s.repo.CreateOrder(ctx, repository.CreateOrderInput{
		UserID:          in.UserID,
		RestaurantID:    in.RestaurantID,
		DeliveryAddress: in.DeliveryAddress,
		IdempotencyKey:  in.IdempotencyKey,
		RequestHash:     canonicalRequestHash(in),
		Items:           repoItems,
	})
	if err != nil {
		return domain.Order{}, false, err
	}
	if !existed {
		s.pub.PublishOrderCreated(ctx, order)
	}
	return order, existed, nil
}

// GetUserOrder возвращает карточку заказа. Чужой заказ трактуется как
// ErrNotFound — не раскрываем существование id, к которому у клиента
// нет доступа.
func (s *Service) GetUserOrder(ctx context.Context, userID string, id uuid.UUID) (domain.Order, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Order{}, fmt.Errorf("%w: не передан заголовок X-User-Id", domain.ErrValidation)
	}
	o, err := s.repo.GetOrder(ctx, id)
	if err != nil {
		return domain.Order{}, err
	}
	if o.UserID != userID {
		return domain.Order{}, domain.ErrNotFound
	}
	return o, nil
}

// ListUserOrders возвращает заказы пользователя с постраничным выводом.
func (s *Service) ListUserOrders(ctx context.Context, userID string, limit, offset int) ([]domain.Order, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: не передан заголовок X-User-Id", domain.ErrValidation)
	}
	return s.repo.ListUserOrders(ctx, userID, limit, offset)
}

// ListRestaurantOrders возвращает заказы заведения. Пустой status
// означает «все статусы».
func (s *Service) ListRestaurantOrders(ctx context.Context, restaurantID uuid.UUID, status string, limit int) ([]domain.Order, error) {
	if status != "" && !domain.OrderStatus(status).Valid() {
		return nil, fmt.Errorf("%w: неизвестный статус %q", domain.ErrValidation, status)
	}
	return s.repo.ListRestaurantOrders(ctx, restaurantID, status, limit)
}

// canonicalRequestHash — SHA-256 (hex) от нормализованного представления
// запроса: restaurant_id + delivery_address + агрегированный по
// menu_item_id список позиций (отсортированный, чтобы порядок в клиенте
// не влиял на хэш). Дубликаты позиций считаются как одна с суммарным
// quantity — то же, что делает CreateOrder внутри транзакции. Только
// такой хэш даёт настоящую защиту от переиспользования ключа с
// «другим» телом.
func canonicalRequestHash(in CreateOrderInput) string {
	agg := map[uuid.UUID]int{}
	for _, it := range in.Items {
		agg[it.MenuItemID] += it.Quantity
	}
	ids := make([]uuid.UUID, 0, len(agg))
	for id := range agg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	type pair struct {
		ID  string `json:"id"`
		Qty int    `json:"q"`
	}
	items := make([]pair, len(ids))
	for i, id := range ids {
		items[i] = pair{ID: id.String(), Qty: agg[id]}
	}
	body, _ := json.Marshal(struct {
		R string `json:"r"`
		A string `json:"a"`
		I []pair `json:"i"`
	}{R: in.RestaurantID.String(), A: strings.TrimSpace(in.DeliveryAddress), I: items})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// CancelOrder отменяет заказ по инициативе пользователя. Разрешается
// только для не-терминальных ранних статусов (created/accepted). Для
// cooking/ready решение по отмене принимает заведение; delivered и
// cancelled — терминалы. Публикует orders.status.cancelled.
func (s *Service) CancelOrder(ctx context.Context, userID string, id uuid.UUID, reason string) (domain.Order, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Order{}, fmt.Errorf("%w: не передан заголовок X-User-Id", domain.ErrValidation)
	}
	current, err := s.repo.GetOrder(ctx, id)
	if err != nil {
		return domain.Order{}, err
	}
	if current.UserID != userID {
		return domain.Order{}, domain.ErrNotFound
	}
	switch current.Status {
	case domain.OrderStatusCreated, domain.OrderStatusAccepted:
		// разрешено пользователю
	default:
		return domain.Order{}, fmt.Errorf("%w: заказ уже %s, отменить может только заведение",
			domain.ErrForbiddenTransition, current.Status)
	}
	comment := strings.TrimSpace(reason)
	if comment == "" {
		comment = "отменено пользователем"
	}
	order, changed, err := s.repo.UpdateOrderStatus(ctx, current.RestaurantID, id, domain.OrderStatusCancelled, comment)
	if err != nil {
		return domain.Order{}, err
	}
	if changed {
		s.pub.PublishOrderStatusChanged(ctx, order, comment)
	}
	return order, nil
}

// GetOrderHistory возвращает список смен статуса заказа. Пользователь
// видит только историю своего заказа.
func (s *Service) GetOrderHistory(ctx context.Context, userID string, id uuid.UUID) ([]domain.OrderStatusEvent, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: не передан заголовок X-User-Id", domain.ErrValidation)
	}
	o, err := s.repo.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return s.repo.ListOrderStatusHistory(ctx, id)
}

// UpdateOrderStatus меняет статус заказа и публикует событие
// orders.status.<new> — только если статус реально сменился. No-op PATCH
// на уже-целевой статус событие не публикует, иначе дубль PATCH рождал
// бы дубль события, а sample порождал бы усиливающийся event-цикл
// (см. ADR-C4 в architecture-plan.md).
func (s *Service) UpdateOrderStatus(ctx context.Context, restaurantID, orderID uuid.UUID, next domain.OrderStatus, comment string) (domain.Order, error) {
	if !next.Valid() {
		return domain.Order{}, fmt.Errorf("%w: неизвестный статус %q", domain.ErrValidation, next)
	}
	if next == domain.OrderStatusCreated {
		return domain.Order{}, fmt.Errorf("%w: нельзя вернуть заказ в статус 'created'", domain.ErrForbiddenTransition)
	}
	order, changed, err := s.repo.UpdateOrderStatus(ctx, restaurantID, orderID, next, comment)
	if err != nil {
		return domain.Order{}, err
	}
	if changed {
		s.pub.PublishOrderStatusChanged(ctx, order, comment)
	}
	return order, nil
}
