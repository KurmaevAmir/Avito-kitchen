// Package logger конструирует общий slog-логгер для всех бинарников
// проекта. Единый формат JSON — чтобы логи из api, migrate и
// restaurant-sample можно было агрегировать одним запросом.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New возвращает slog-логгер с JSON-выводом в stdout и уровнем,
// заданным строкой (debug/info/warn/error). Неизвестное значение
// трактуется как info.
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
