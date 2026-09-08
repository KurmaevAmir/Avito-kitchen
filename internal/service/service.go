// Package service содержит бизнес-логику Авито.Кухни. От инфраструктуры
// зависит только через интерфейсы (Repository/Cache/Publisher), которые
// объявлены в interfaces.go — по Dependency Inversion Principle
// абстракции определяет потребитель.
package service

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/cache"
	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/events"
)

// TTL кэша: витрина и меню меняются редко, но читаются часто.
const (
	restaurantsTTL = 60 * time.Second
	restaurantTTL  = 60 * time.Second
	menuTTL        = 30 * time.Second
)

// Service — фасад бизнес-логики Авито.Кухни.
type Service struct {
	repo  Repository
	cache Cache
	pub   Publisher
}

// Options — необязательные зависимости. Nil-значения заменяются на
// no-op реализации из сопряжённых пакетов.
type Options struct {
	Cache     Cache
	Publisher Publisher
}

// New собирает сервис.
func New(repo Repository, opts Options) *Service {
	c := opts.Cache
	if c == nil {
		c = cache.NewNoop()
	}
	p := opts.Publisher
	if p == nil {
		p = events.NewNoopPublisher()
	}
	return &Service{repo: repo, cache: c, pub: p}
}

// Ключи кэша. Фильтр открытости входит в ключ списка — иначе смена
// фильтра могла бы вернуть stale-данные предыдущего запроса.
func keyRestaurantsList(filter domain.OpenFilter, limit, offset int) string {
	return fmt.Sprintf("restaurants:list:f=%s:l=%d:o=%d", filter, limit, offset)
}
func keyRestaurant(id uuid.UUID) string { return "restaurant:" + id.String() }
func keyMenu(restaurantID uuid.UUID) string {
	return "restaurant:" + restaurantID.String() + ":menu"
}
