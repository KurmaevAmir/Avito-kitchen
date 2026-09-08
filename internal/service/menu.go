package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// GetMenu возвращает публичное меню заведения (только доступные и
// неудалённые позиции). Отдельно проверяет существование самого
// заведения — иначе клиент не отличил бы «нет такого заведения» от
// «меню пустое».
func (s *Service) GetMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error) {
	if _, err := s.GetRestaurant(ctx, restaurantID); err != nil {
		return nil, err
	}
	key := keyMenu(restaurantID)
	var cached []domain.MenuItem
	if hit, _ := s.cache.Get(ctx, key, &cached); hit {
		return cached, nil
	}
	items, err := s.repo.ListAvailableMenu(ctx, restaurantID)
	if err != nil {
		return nil, err
	}
	_ = s.cache.Set(ctx, key, items, menuTTL)
	return items, nil
}

// AuthenticatePartner ищет заведение по bearer-токену. В БД хранится
// только SHA-256 (hex) хэш — сравниваем по нему. И пустой, и неверный
// токен возвращают ErrUnauthorized: транспортный слой поставит 401 +
// WWW-Authenticate.
func (s *Service) AuthenticatePartner(ctx context.Context, apiKey string) (domain.Restaurant, error) {
	if strings.TrimSpace(apiKey) == "" {
		return domain.Restaurant{}, fmt.Errorf("%w: не передан bearer-токен", domain.ErrUnauthorized)
	}
	sum := sha256.Sum256([]byte(apiKey))
	hash := hex.EncodeToString(sum[:])
	rest, err := s.repo.GetRestaurantByAPIKeyHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Restaurant{}, fmt.Errorf("%w: неверный bearer-токен", domain.ErrUnauthorized)
		}
		return domain.Restaurant{}, err
	}
	return rest, nil
}

// PartnerMenu возвращает всё меню заведения, включая недоступные
// позиции. Не кэшируется: партнёру нужны свежие данные.
func (s *Service) PartnerMenu(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuItem, error) {
	return s.repo.ListAllMenu(ctx, restaurantID)
}

// AddMenuItem добавляет позицию в меню и инвалидирует публичный кэш меню.
func (s *Service) AddMenuItem(ctx context.Context, item domain.MenuItem) (domain.MenuItem, error) {
	if strings.TrimSpace(item.Name) == "" {
		return domain.MenuItem{}, fmt.Errorf("%w: имя позиции обязательно", domain.ErrValidation)
	}
	if item.Price < 0 {
		return domain.MenuItem{}, fmt.Errorf("%w: цена не может быть отрицательной", domain.ErrValidation)
	}
	if item.WeightGrams != nil && *item.WeightGrams < 0 {
		return domain.MenuItem{}, fmt.Errorf("%w: вес не может быть отрицательным", domain.ErrValidation)
	}
	if item.Allergens == nil {
		item.Allergens = []string{}
	}
	created, err := s.repo.CreateMenuItem(ctx, item)
	if err != nil {
		return domain.MenuItem{}, err
	}
	_ = s.cache.Del(ctx, keyMenu(item.RestaurantID))
	return created, nil
}

// PatchMenuItem обновляет цену и/или available. Требует хотя бы одно
// поле, иначе PATCH превратился бы в бессмысленный no-op.
func (s *Service) PatchMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID, price *int64, available *bool) (domain.MenuItem, error) {
	if price != nil && *price < 0 {
		return domain.MenuItem{}, fmt.Errorf("%w: цена не может быть отрицательной", domain.ErrValidation)
	}
	if price == nil && available == nil {
		return domain.MenuItem{}, fmt.Errorf("%w: нужно передать хотя бы одно поле", domain.ErrValidation)
	}
	item, err := s.repo.PatchMenuItem(ctx, restaurantID, itemID, price, available)
	if err != nil {
		return domain.MenuItem{}, err
	}
	_ = s.cache.Del(ctx, keyMenu(restaurantID))
	return item, nil
}

// DeleteMenuItem мягко удаляет позицию (deleted_at = NOW()). Идемпотентна.
func (s *Service) DeleteMenuItem(ctx context.Context, restaurantID, itemID uuid.UUID) error {
	if err := s.repo.SoftDeleteMenuItem(ctx, restaurantID, itemID); err != nil {
		return err
	}
	_ = s.cache.Del(ctx, keyMenu(restaurantID))
	return nil
}
