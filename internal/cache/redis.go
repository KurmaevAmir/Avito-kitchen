// Package cache — реализации кэша, требуемого сервисом. Абстракцию
// (интерфейс service.Cache) держит потребитель; здесь только Redis
// и no-op.
//
// Обе реализации fail-open: ошибки Redis (недоступность, битые данные)
// логгируются и трактуются как «miss» — кэш не роняет основной путь.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache — реализация кэша поверх go-redis.
type RedisCache struct {
	rdb *redis.Client
	log *slog.Logger
}

// NewRedis подключается к Redis и пингует его. Пинг после подключения
// заставляет упасть быстро, а не при первом Get в проде.
func NewRedis(ctx context.Context, url string, log *slog.Logger) (*RedisCache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &RedisCache{rdb: rdb, log: log}, nil
}

// Get разбирает JSON-значение по ключу в dst. hit=false, если ключа нет
// или Redis недоступен; err не-nil только при программной ошибке.
func (c *RedisCache) Get(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		c.log.Warn("не удалось прочитать из кэша", "key", key, "err", err.Error())
		return false, nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		// Битая запись — удалим, чтобы следующий Get пошёл в БД и
		// прогрел ключ заново.
		_ = c.rdb.Del(ctx, key).Err()
		return false, nil
	}
	return true, nil
}

// Set кладёт значение в кэш с TTL.
func (c *RedisCache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	if err := c.rdb.Set(ctx, key, raw, ttl).Err(); err != nil {
		c.log.Warn("не удалось записать в кэш", "key", key, "err", err.Error())
		return err
	}
	return nil
}

// Del удаляет ключи. Используется для инвалидации после mutation'ов.
func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.log.Warn("не удалось удалить ключи кэша", "keys", keys, "err", err.Error())
		return err
	}
	return nil
}

// Close закрывает соединение с Redis.
func (c *RedisCache) Close() error { return c.rdb.Close() }

// NoopCache — реализация-заглушка: Get всегда miss, Set/Del — no-op.
// Используется, когда Redis не сконфигурирован или недоступен.
type NoopCache struct{}

// NewNoop возвращает заглушку кэша.
func NewNoop() NoopCache { return NoopCache{} }

// Get всегда возвращает miss (hit=false).
func (NoopCache) Get(context.Context, string, any) (bool, error) { return false, nil }

// Set — no-op.
func (NoopCache) Set(context.Context, string, any, time.Duration) error { return nil }

// Del — no-op.
func (NoopCache) Del(context.Context, ...string) error { return nil }

// Close — no-op.
func (NoopCache) Close() error { return nil }
