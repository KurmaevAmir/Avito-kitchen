package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// ListAvailableMenu возвращает публичное меню: только available=true и
// не soft-deleted.
func (r *Repository) ListAvailableMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error) {
	return r.listMenu(ctx, restaurantID, true)
}

// ListAllMenu возвращает партнёрское меню: все не soft-deleted позиции,
// включая available=false.
func (r *Repository) ListAllMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error) {
	return r.listMenu(ctx, restaurantID, false)
}

func (r *Repository) listMenu(ctx context.Context, restaurantID uuid.UUID, onlyAvailable bool) ([]domain.MenuItem, error) {
	q := `SELECT id, restaurant_id, name, description, price, weight_grams, composition, allergens, available, created_at
	      FROM menu_items
	      WHERE restaurant_id = $1 AND deleted_at IS NULL`
	if onlyAvailable {
		q += ` AND available = TRUE`
	}
	q += ` ORDER BY created_at ASC`
	rows, err := r.pool.Query(ctx, q, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MenuItem
	for rows.Next() {
		var it domain.MenuItem
		if err := rows.Scan(&it.ID, &it.RestaurantID, &it.Name, &it.Description,
			&it.Price, &it.WeightGrams, &it.Composition, &it.Allergens,
			&it.Available, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CreateMenuItem вставляет позицию и возвращает её со сгенерированным
// сервером id и created_at.
func (r *Repository) CreateMenuItem(ctx context.Context, m domain.MenuItem) (domain.MenuItem, error) {
	const q = `
		INSERT INTO menu_items (restaurant_id, name, description, price, weight_grams, composition, allergens, available)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`
	err := r.pool.QueryRow(ctx, q,
		m.RestaurantID, m.Name, m.Description, m.Price,
		m.WeightGrams, m.Composition, m.Allergens, m.Available,
	).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// PatchMenuItem обновляет цену и/или available. nil-значения оставляют
// поле без изменений (COALESCE в SQL).
func (r *Repository) PatchMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID, price *int64, available *bool) (domain.MenuItem, error) {
	const q = `
		UPDATE menu_items
		SET price     = COALESCE($3, price),
		    available = COALESCE($4, available)
		WHERE id = $1 AND restaurant_id = $2 AND deleted_at IS NULL
		RETURNING id, restaurant_id, name, description, price, weight_grams, composition, allergens, available, created_at`
	var it domain.MenuItem
	err := r.pool.QueryRow(ctx, q, itemID, restaurantID, price, available).
		Scan(&it.ID, &it.RestaurantID, &it.Name, &it.Description,
			&it.Price, &it.WeightGrams, &it.Composition, &it.Allergens,
			&it.Available, &it.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MenuItem{}, domain.ErrNotFound
	}
	return it, err
}

// SoftDeleteMenuItem помечает позицию как удалённую. Идемпотентна:
// повторный вызов на уже удалённой возвращает nil.
func (r *Repository) SoftDeleteMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID) error {
	const q = `
		UPDATE menu_items
		SET deleted_at = NOW()
		WHERE id = $1 AND restaurant_id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, itemID, restaurantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// Не обновили ни одной строки. Отличаем «нет такой позиции» от
	// «позиция уже удалена» — иначе не поддержим идемпотентность DELETE.
	var exists bool
	err = r.pool.QueryRow(ctx,
		`SELECT TRUE FROM menu_items WHERE id = $1 AND restaurant_id = $2`,
		itemID, restaurantID,
	).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
