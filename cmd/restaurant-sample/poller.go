package main

import (
	"context"
	"log/slog"
	"time"
)

// runPolling — fallback-режим, если RabbitMQ недоступен. Опрашивает
// partner-API и продвигает нетерминальные заказы на шаг вперёд.
func runPolling(ctx context.Context, log *slog.Logger, api *apiClient, cfg config) {
	log.Info("restaurant-sample запущен в режиме polling",
		"api", cfg.apiBase, "poll", cfg.pollEvery.String())
	ticker := time.NewTicker(cfg.pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("restaurant-sample останавливается")
			return
		case <-ticker.C:
			pollTick(ctx, api, cfg)
		}
	}
}

func pollTick(ctx context.Context, api *apiClient, cfg config) {
	for _, o := range api.fetchByStatus(ctx, "created") {
		api.patchStatus(ctx, o.ID, "accepted", "авто-подтверждено демо-кухней (polling)")
	}
	for _, o := range api.fetchByStatus(ctx, "accepted") {
		api.patchStatus(ctx, o.ID, "cooking", "начали готовить")
	}
	for _, o := range api.fetchByStatus(ctx, "cooking") {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.cookDelay):
		}
		api.patchStatus(ctx, o.ID, "ready", "приготовлено и упаковано")
	}
	for _, o := range api.fetchByStatus(ctx, "ready") {
		api.patchStatus(ctx, o.ID, "delivered", "передано курьеру")
	}
}
