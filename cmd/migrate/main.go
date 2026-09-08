// Команда migrate применяет SQL-миграции из каталога ./migrations
// через библиотеку goose к БД из переменной DATABASE_URL. Отдельный
// бинарник, чтобы docker-compose запускал его как one-shot job и
// api стартовал только после успешной миграции.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует драйвер pgx для database/sql
	"github.com/pressly/goose/v3"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error("переменная окружения DATABASE_URL обязательна")
		os.Exit(1)
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "/app/migrations"
	}

	if err := run(dsn, dir, log); err != nil {
		log.Error("миграции упали", "err", err.Error())
		os.Exit(1)
	}
	log.Info("миграции применены")
}

// run отделён от main, чтобы defer db.Close() отработал до os.Exit.
func run(dsn, dir string, log *slog.Logger) error {
	db, err := waitForDB(context.Background(), dsn, log)
	if err != nil {
		return fmt.Errorf("подключение к БД: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("диалект goose: %w", err)
	}
	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// waitForDB ждёт готовности БД до 30 секунд с шагом 1 секунда.
func waitForDB(ctx context.Context, dsn string, log *slog.Logger) (*sql.DB, error) {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", dsn)
		if err == nil {
			if pingErr := db.PingContext(ctx); pingErr == nil {
				return db, nil
			} else {
				lastErr = pingErr
				_ = db.Close()
			}
		} else {
			lastErr = err
		}
		log.Info("БД пока не готова, повторяем попытку", "err", lastErr.Error())
		time.Sleep(1 * time.Second)
	}
	return nil, lastErr
}
