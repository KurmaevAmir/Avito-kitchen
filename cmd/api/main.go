// Команда kitchen-api — HTTP-сервис Авито.Кухни: подключается к БД,
// опционально к Redis и RabbitMQ, поднимает Gin, ждёт SIGINT/SIGTERM
// и завершает работу gracefully.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kurmaevamir/avito-kitchen/internal/cache"
	"github.com/kurmaevamir/avito-kitchen/internal/config"
	"github.com/kurmaevamir/avito-kitchen/internal/events"
	"github.com/kurmaevamir/avito-kitchen/internal/logger"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
	"github.com/kurmaevamir/avito-kitchen/internal/service"
	httpapi "github.com/kurmaevamir/avito-kitchen/internal/transport/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Логгер ещё не сконфигурирован — падаем с явной stderr-строкой,
		// иначе не будет понятно, что упало на самом раннем этапе.
		fmt.Fprintln(os.Stderr, "не удалось загрузить конфигурацию:", err)
		os.Exit(1)
	}
	log := logger.New(cfg.LogLevel)

	// Свои структурные JSON-логи пишем через slog, per-request дебаг-
	// вывод gin отключаем.
	gin.SetMode(gin.ReleaseMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := connectDB(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Error("не удалось подключиться к БД", "err", err)
		cancel()
		os.Exit(1) //nolint:gocritic // defer уже отработал ручным cancel()
	}
	defer pool.Close()

	// Тип переменной — service.Cache (интерфейс у потребителя): позволяет
	// присвоить и NoopCache, и *RedisCache.
	var rcache service.Cache = cache.NewNoop()
	if cfg.RedisURL != "" {
		if c, err := cache.NewRedis(ctx, cfg.RedisURL, log); err != nil {
			log.Warn("Redis недоступен, кэширование недоступно", "err", err.Error())
		} else {
			rcache = c
			defer func() { _ = rcache.Close() }()
			log.Info("Redis подключён", "url", cfg.RedisURL)
		}
	}

	var publisher service.Publisher = events.NewNoopPublisher()
	if cfg.RabbitURL != "" {
		if p, err := events.NewRabbitPublisher(cfg.RabbitURL, log); err != nil {
			log.Warn("RabbitMQ недоступен, публикации событий недоступны", "err", err.Error())
		} else {
			publisher = p
			defer func() { _ = publisher.Close() }()
			log.Info("RabbitMQ подключён", "url", cfg.RabbitURL)
		}
	}

	repo := repository.New(pool)
	svc := service.New(repo, service.Options{Cache: rcache, Publisher: publisher})
	router := httpapi.NewRouter(httpapi.Deps{
		Service:          svc,
		Logger:           log,
		Pinger:           repo,
		OpenAPIPath:      cfg.OpenAPIPath,
		CORSOrigins:      cfg.CORSOrigins,
		ValidateRequests: cfg.ValidateRequests,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("HTTP-сервер слушает", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("ошибка HTTP-сервера", "err", err)
			cancel()
		}
	}()

	<-shutdown
	log.Info("получен сигнал завершения — начинаем graceful shutdown")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("ошибка при остановке HTTP-сервера", "err", err)
	}
	log.Info("сервер остановлен")
}

// connectDB создаёт пул pgx с ретраями. Postgres в compose стартует
// не мгновенно — без ретраев api падал бы при холодном старте.
func connectDB(ctx context.Context, url string, log interface {
	Info(msg string, args ...any)
}) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2

	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				return pool, nil
			} else {
				lastErr = pingErr
				pool.Close()
			}
		} else {
			lastErr = err
		}
		log.Info("БД пока не готова, повторяем попытку", "err", lastErr.Error())
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
