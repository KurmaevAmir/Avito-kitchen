package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// CreateOrderInput — входные данные транзакции создания заказа.
// RequestHash — SHA-256 hex от канонизированного тела запроса; при
// повторном POST с тем же IdempotencyKey и **другим** RequestHash
// возвращаем ErrIdempotencyMismatch (IETF idempotency draft).
type CreateOrderInput struct {
	UserID          string
	RestaurantID    uuid.UUID
	DeliveryAddress string
	IdempotencyKey  string
	RequestHash     string
	Items           []CreateOrderItem
}

// CreateOrderItem — позиция заказа без цены. Цена берётся из меню
// внутри транзакции, чтобы клиент не мог подсунуть свою.
type CreateOrderItem struct {
	MenuItemID uuid.UUID
	Quantity   int
}

// CreateOrder создаёт заказ атомарно вместе с позициями и первой
// записью истории статусов. Второй возвращаемый параметр — признак
// идемпотентного повтора: при повторном POST с тем же
// (UserID, IdempotencyKey) возвращается ранее созданный заказ.
func (r *Repository) CreateOrder(ctx context.Context, in CreateOrderInput) (domain.Order, bool, error) {
	// Быстрый путь идемпотентности — без открытия транзакции.
	if existing, existingHash, err := r.findOrderByIdempotency(ctx, in.UserID, in.IdempotencyKey); err == nil {
		if existingHash != in.RequestHash {
			return domain.Order{}, false, domain.ErrIdempotencyMismatch
		}
		return existing, true, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Order{}, false, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Order{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var isOpen bool
	err = tx.QueryRow(ctx,
		`SELECT is_open FROM restaurants WHERE id = $1 AND deleted_at IS NULL`,
		in.RestaurantID,
	).Scan(&isOpen)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, false, domain.ErrNotFound
		}
		return domain.Order{}, false, err
	}
	if !isOpen {
		return domain.Order{}, false, domain.ErrRestaurantClosed
	}

	// FOR SHARE защищает от параллельного PATCH /partner/menu, но не
	// блокирует параллельные чтения. Дубликаты menu_item_id клиент
	// вправе прислать — агрегируем количество здесь.
	qty := make(map[uuid.UUID]int, len(in.Items))
	ids := make([]uuid.UUID, 0, len(in.Items))
	for _, it := range in.Items {
		if _, seen := qty[it.MenuItemID]; !seen {
			ids = append(ids, it.MenuItemID)
		}
		qty[it.MenuItemID] += it.Quantity
	}
	rows, err := tx.Query(ctx, `
		SELECT id, restaurant_id, name, price, available
		FROM menu_items
		WHERE id = ANY($1) AND deleted_at IS NULL
		FOR SHARE`, ids)
	if err != nil {
		return domain.Order{}, false, err
	}
	type mi struct {
		id           uuid.UUID
		restaurantID uuid.UUID
		name         string
		price        int64
		available    bool
	}
	fetched := make(map[uuid.UUID]mi, len(ids))
	for rows.Next() {
		var m mi
		if err := rows.Scan(&m.id, &m.restaurantID, &m.name, &m.price, &m.available); err != nil {
			rows.Close()
			return domain.Order{}, false, err
		}
		fetched[m.id] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Order{}, false, err
	}
	if len(fetched) != len(ids) {
		return domain.Order{}, false, fmt.Errorf("%w: часть позиций меню не найдена или удалена", domain.ErrNotFound)
	}
	var total int64
	for id, m := range fetched {
		if m.restaurantID != in.RestaurantID {
			return domain.Order{}, false, domain.ErrMismatchedRestaurant
		}
		if !m.available {
			return domain.Order{}, false, fmt.Errorf("%w: %s", domain.ErrItemUnavailable, m.name)
		}
		total += m.price * int64(qty[id])
	}

	var orderID uuid.UUID
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (user_id, restaurant_id, status, total_price, delivery_address, idempotency_key, request_hash)
		VALUES ($1,$2,'created',$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`,
		in.UserID, in.RestaurantID, total, in.DeliveryAddress, in.IdempotencyKey, in.RequestHash,
	).Scan(&orderID, &createdAt, &updatedAt)
	if err != nil {
		// Гонка на UNIQUE(user_id, idempotency_key): параллельный POST
		// успел вставить раньше нас — проверяем совпадение request_hash.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_ = tx.Rollback(ctx)
			existing, existingHash, findErr := r.findOrderByIdempotency(ctx, in.UserID, in.IdempotencyKey)
			if findErr != nil {
				return domain.Order{}, false, findErr
			}
			if existingHash != in.RequestHash {
				return domain.Order{}, false, domain.ErrIdempotencyMismatch
			}
			return existing, true, nil
		}
		return domain.Order{}, false, err
	}

	// Одна строка order_items на уникальный menu_item_id — суммарное
	// количество. Идём по ids (стабильный порядок), чтобы не зависеть
	// от рандомизации итерации map.
	items := make([]domain.OrderItem, 0, len(ids))
	for _, id := range ids {
		m := fetched[id]
		var itemID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO order_items (order_id, menu_item_id, name, price, quantity)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			orderID, m.id, m.name, m.price, qty[id],
		).Scan(&itemID); err != nil {
			return domain.Order{}, false, err
		}
		items = append(items, domain.OrderItem{
			ID: itemID, OrderID: orderID, MenuItemID: m.id,
			Name: m.name, Price: m.price, Quantity: qty[id],
		})
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO order_status_history (order_id, status, comment) VALUES ($1,'created','')`,
		orderID); err != nil {
		return domain.Order{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, false, err
	}

	return domain.Order{
		ID:           orderID,
		UserID:       in.UserID,
		RestaurantID: in.RestaurantID,
		Status:       domain.OrderStatusCreated,
		TotalPrice:   total,
		DeliveryAddr: in.DeliveryAddress,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		Items:        items,
	}, false, nil
}

// findOrderByIdempotency возвращает существующий заказ по паре
// (userID, idempotencyKey) и его сохранённый request_hash. Пустой третий
// возврат для не найденных.
func (r *Repository) findOrderByIdempotency(ctx context.Context, userID, key string) (domain.Order, string, error) {
	const q = `SELECT id, request_hash FROM orders WHERE user_id = $1 AND idempotency_key = $2`
	var id uuid.UUID
	var reqHash string
	err := r.pool.QueryRow(ctx, q, userID, key).Scan(&id, &reqHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, "", domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, "", err
	}
	o, err := r.GetOrder(ctx, id)
	return o, reqHash, err
}

// GetOrder возвращает заказ по идентификатору со всеми его позициями.
func (r *Repository) GetOrder(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	const q = `
		SELECT id, user_id, restaurant_id, status, total_price, delivery_address, created_at, updated_at
		FROM orders WHERE id = $1`
	var o domain.Order
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.UserID, &o.RestaurantID, &o.Status, &o.TotalPrice, &o.DeliveryAddr, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, err
	}
	items, err := r.listOrderItems(ctx, id)
	if err != nil {
		return domain.Order{}, err
	}
	o.Items = items
	return o, nil
}

func (r *Repository) listOrderItems(ctx context.Context, orderID uuid.UUID) ([]domain.OrderItem, error) {
	const q = `SELECT id, order_id, menu_item_id, name, price, quantity FROM order_items WHERE order_id = $1`
	rows, err := r.pool.Query(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrderItem
	for rows.Next() {
		var it domain.OrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.Name, &it.Price, &it.Quantity); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListOrderStatusHistory возвращает историю смен статуса заказа
// в хронологическом порядке.
func (r *Repository) ListOrderStatusHistory(ctx context.Context, orderID uuid.UUID) ([]domain.OrderStatusEvent, error) {
	const q = `SELECT status, comment, changed_at
	           FROM order_status_history WHERE order_id = $1
	           ORDER BY changed_at ASC`
	rows, err := r.pool.Query(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.OrderStatusEvent{}
	for rows.Next() {
		var e domain.OrderStatusEvent
		e.OrderID = orderID
		if err := rows.Scan(&e.Status, &e.Comment, &e.ChangedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListUserOrders возвращает заказы пользователя с постраничным выводом,
// свежие сверху.
func (r *Repository) ListUserOrders(ctx context.Context, userID string, limit, offset int) ([]domain.Order, error) {
	const q = `
		SELECT id, user_id, restaurant_id, status, total_price, delivery_address, created_at, updated_at
		FROM orders WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`
	return r.queryOrdersWithItems(ctx, q, userID, limit, offset)
}

// ListRestaurantOrders возвращает заказы заведения. Пустой status
// означает «все статусы».
func (r *Repository) ListRestaurantOrders(ctx context.Context, restaurantID uuid.UUID, status string, limit int) ([]domain.Order, error) {
	if status == "" {
		const q = `
			SELECT id, user_id, restaurant_id, status, total_price, delivery_address, created_at, updated_at
			FROM orders WHERE restaurant_id = $1
			ORDER BY created_at DESC LIMIT $2`
		return r.queryOrdersWithItems(ctx, q, restaurantID, limit)
	}
	const q = `
		SELECT id, user_id, restaurant_id, status, total_price, delivery_address, created_at, updated_at
		FROM orders WHERE restaurant_id = $1 AND status = $2
		ORDER BY created_at DESC LIMIT $3`
	return r.queryOrdersWithItems(ctx, q, restaurantID, status, limit)
}

func (r *Repository) queryOrdersWithItems(ctx context.Context, q string, args ...any) ([]domain.Order, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	orders := []domain.Order{}
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.RestaurantID, &o.Status, &o.TotalPrice, &o.DeliveryAddr, &o.CreatedAt, &o.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return orders, nil
	}
	// Один запрос IN на все позиции вместо N отдельных.
	ids := make([]uuid.UUID, len(orders))
	byOrder := make(map[uuid.UUID]int, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
		byOrder[o.ID] = i
	}
	itemRows, err := r.pool.Query(ctx, `
		SELECT id, order_id, menu_item_id, name, price, quantity
		FROM order_items
		WHERE order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it domain.OrderItem
		if err := itemRows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.Name, &it.Price, &it.Quantity); err != nil {
			return nil, err
		}
		if idx, ok := byOrder[it.OrderID]; ok {
			orders[idx].Items = append(orders[idx].Items, it)
		}
	}
	return orders, itemRows.Err()
}

// UpdateOrderStatus атомарно проверяет текущий статус, валидирует
// переход и обновляет статус с записью в историю. Второй возвращаемый
// параметр — changed: true, если статус реально сменился; false, если
// заказ уже был в целевом статусе (no-op). Сервис по changed решает,
// публиковать ли событие — иначе на дубле PATCH получаем усиливающийся
// event-цикл.
//
// Заказ чужого ресторана возвращает ErrNotFound: не раскрываем чужие
// идентификаторы через 403.
func (r *Repository) UpdateOrderStatus(ctx context.Context, restaurantID, orderID uuid.UUID, next domain.OrderStatus, comment string) (domain.Order, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Order{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentStatus domain.OrderStatus
	var owningRestaurant uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT status, restaurant_id FROM orders WHERE id = $1 FOR UPDATE`, orderID,
	).Scan(&currentStatus, &owningRestaurant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, false, err
	}
	if owningRestaurant != restaurantID {
		return domain.Order{}, false, domain.ErrNotFound
	}
	if currentStatus == next {
		if err := tx.Commit(ctx); err != nil {
			return domain.Order{}, false, err
		}
		order, err := r.GetOrder(ctx, orderID)
		return order, false, err
	}
	if !currentStatus.CanTransitionTo(next) {
		return domain.Order{}, false, fmt.Errorf("%w: %s -> %s", domain.ErrForbiddenTransition, currentStatus, next)
	}
	// RETURNING внутри транзакции: свежие данные без второго round-trip
	// и без риска гонки после commit.
	var updated domain.Order
	err = tx.QueryRow(ctx, `
		UPDATE orders SET status = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, user_id, restaurant_id, status, total_price, delivery_address, created_at, updated_at`,
		next, orderID,
	).Scan(&updated.ID, &updated.UserID, &updated.RestaurantID, &updated.Status,
		&updated.TotalPrice, &updated.DeliveryAddr, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		return domain.Order{}, false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO order_status_history (order_id, status, comment) VALUES ($1,$2,$3)`,
		orderID, next, comment); err != nil {
		return domain.Order{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, false, err
	}
	items, err := r.listOrderItems(ctx, orderID)
	if err != nil {
		return domain.Order{}, false, err
	}
	updated.Items = items
	return updated, true, nil
}
