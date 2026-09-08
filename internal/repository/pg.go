// Package repository — доступ к PostgreSQL через pgx. Реализует
// интерфейс service.Repository. Все чтения по таблицам с soft-delete
// фильтруют WHERE deleted_at IS NULL.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository — обёртка над пулом pgx.
type Repository struct {
	pool *pgxpool.Pool
}

// New создаёт репозиторий поверх готового пула соединений pgx.
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Ping проверяет доступность БД. Используется readiness-эндпоинтом.
func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}
