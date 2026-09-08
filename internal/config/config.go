// Package config читает конфигурацию сервиса из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config — конфигурация процесса. RedisURL и RabbitURL опциональны:
// пустые значения означают, что сервис работает без кэша и без
// публикации событий (см. no-op реализации).
type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	RedisURL         string
	RabbitURL        string
	OpenAPIPath      string
	CORSOrigins      []string
	ValidateRequests bool
	ShutdownTimeout  time.Duration
	LogLevel         string
}

// Load загружает конфигурацию из окружения. DATABASE_URL обязателен.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		RedisURL:         getEnv("REDIS_URL", ""),
		RabbitURL:        getEnv("RABBITMQ_URL", ""),
		OpenAPIPath:      getEnv("OPENAPI_PATH", "/app/openapi.yaml"),
		CORSOrigins:      splitCSV(getEnv("CORS_ORIGINS", "")),
		ValidateRequests: getEnv("OPENAPI_VALIDATE", "true") == "true",
		ShutdownTimeout:  getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("переменная окружения DATABASE_URL обязательна")
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitCSV разбивает "a, b,  c" в []string{"a", "b", "c"}. Пустое
// значение даёт nil, что означает «CORS выключен».
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// getDuration принимает и целое число секунд ("10"), и полноценный
// формат time.Duration ("10s", "500ms").
func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
