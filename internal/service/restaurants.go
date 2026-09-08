package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// ListRestaurants возвращает заведения по фильтру открытости. Пустой
// filter трактуется как «только открытые».
func (s *Service) ListRestaurants(ctx context.Context, filter domain.OpenFilter, limit, offset int) ([]domain.Restaurant, error) {
	if filter == "" {
		filter = domain.OpenFilterOnlyOpen
	}
	if !filter.Valid() {
		return nil, fmt.Errorf("%w: неизвестное значение is_open %q", domain.ErrValidation, filter)
	}
	key := keyRestaurantsList(filter, limit, offset)
	var cached []domain.Restaurant
	if hit, _ := s.cache.Get(ctx, key, &cached); hit {
		return cached, nil
	}
	items, err := s.repo.ListRestaurants(ctx, filter, limit, offset)
	if err != nil {
		return nil, err
	}
	_ = s.cache.Set(ctx, key, items, restaurantsTTL)
	return items, nil
}

// GetRestaurant возвращает карточку заведения по идентификатору.
func (s *Service) GetRestaurant(ctx context.Context, id uuid.UUID) (domain.Restaurant, error) {
	key := keyRestaurant(id)
	var cached domain.Restaurant
	if hit, _ := s.cache.Get(ctx, key, &cached); hit {
		return cached, nil
	}
	rest, err := s.repo.GetRestaurant(ctx, id)
	if err != nil {
		return domain.Restaurant{}, err
	}
	_ = s.cache.Set(ctx, key, rest, restaurantTTL)
	return rest, nil
}
