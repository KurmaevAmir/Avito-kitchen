// Команда restaurant-sample — демо-сервис заведения, реагирующий на
// события Авито.Кухни. Подписывается на RabbitMQ и через partner-API
// последовательно продвигает заказ по статусам:
//
//	orders.created            → PATCH → accepted
//	orders.status.accepted    → PATCH → cooking
//	orders.status.cooking     → sleep(cook) → PATCH → ready
//	orders.status.ready       → PATCH → delivered
//
// При пустом RABBITMQ_URL откатывается на polling — так демо запускается
// и без брокера.
//
// Файлы:
//
//	main.go       — bootstrap, обработка сигналов, выбор режима
//	config.go     — чтение конфигурации из ENV
//	api_client.go — HTTP-клиент partner-API
//	consumer.go   — event-driven consumer (RabbitMQ)
//	poller.go     — fallback-режим (polling partner-API)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	api := newAPIClient(httpClient, cfg, log)

	if err := api.waitReady(ctx); err != nil {
		log.Error("API недоступен", "err", err)
		return
	}

	if cfg.rabbitURL != "" {
		if err := runEventDriven(ctx, log, api, cfg); err != nil {
			log.Error("event-driven режим упал, выполняем переключение на polling", "err", err.Error())
			runPolling(ctx, log, api, cfg)
		}
		return
	}
	log.Info("RABBITMQ_URL не задан: работа ведется в режиме polling")
	runPolling(ctx, log, api, cfg)
}
