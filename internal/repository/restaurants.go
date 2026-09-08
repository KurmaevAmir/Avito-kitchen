package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// ListRestaurants возвращает заведения по фильтру открытости с
// постраничным выводом. Soft-deleted записи отсекаются.
func (r *Repository) ListRestaurants(ctx context.Context, filter domain.OpenFilter, limit, offset int) ([]domain.Restaurant, error) {
	q := `SELECT id, name, description, address, is_open, created_at
	      FROM restaurants
	      WHERE deleted_at IS NULL`
	switch filter {
	case domain.OpenFilterOnlyOpen:
		q += ` AND is_open = TRUE`
	case domain.OpenFilterOnlyClosed:
		q += ` AND is_open = FALSE`
	}
	q += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Restaurant
	for rows.Next() {
		var it domain.Restaurant
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.Address, &it.IsOpen, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetRestaurant возвращает заведение по идентификатору или ErrNotFound,
// если запись отсутствует или soft-deleted.
func (r *Repository) GetRestaurant(ctx context.Context, id uuid.UUID) (domain.Restaurant, error) {
	const q = `SELECT id, name, description, address, is_open, created_at
	           FROM restaurants
	           WHERE id = $1 AND deleted_at IS NULL`
	var it domain.Restaurant
	err := r.pool.QueryRow(ctx, q, id).Scan(&it.ID, &it.Name, &it.Description, &it.Address, &it.IsOpen, &it.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Restaurant{}, domain.ErrNotFound
	}
	return it, err
}

// GetRestaurantByAPIKeyHash ищет заведение по SHA-256 хэшу bearer-токена.
// Хэш считается в вызывающем коде (сервисе): репозиторий не должен
// знать про алгоритм хэширования.
func (r *Repository) GetRestaurantByAPIKeyHash(ctx context.Context, hash string) (domain.Restaurant, error) {
	const q = `SELECT id, name, description, address, is_open, created_at
	           FROM restaurants
	           WHERE api_key_hash = $1 AND deleted_at IS NULL`
	var it domain.Restaurant
	err := r.pool.QueryRow(ctx, q, hash).Scan(&it.ID, &it.Name, &it.Description, &it.Address, &it.IsOpen, &it.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Restaurant{}, domain.ErrNotFound
	}
	return it, err
}
