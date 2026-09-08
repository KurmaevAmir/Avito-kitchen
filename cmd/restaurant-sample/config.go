package main

import (
	"os"
	"strconv"
	"time"
)

// config — параметры сервиса, читаемые из ENV. Пустой rabbitURL
// переключает сервис в polling-режим.
type config struct {
	apiBase      string
	restKey      string        // bearer-токен партнёра
	restaurantID string        // uuid заведения — используется в routing-key
	rabbitURL    string        // если пусто — работаем в режиме polling
	pollEvery    time.Duration // используется только в polling-режиме
	cookDelay    time.Duration // задержка «готовки» перед статусом ready
	queueName    string        // имя очереди в RabbitMQ
}

func loadConfig() config {
	return config{
		apiBase:      env("API_BASE_URL", "http://api:8080"),
		restKey:      env("RESTAURANT_KEY", "demo-restaurant-key"),
		restaurantID: env("RESTAURANT_ID", "11111111-1111-1111-1111-111111111111"),
		rabbitURL:    os.Getenv("RABBITMQ_URL"),
		pollEvery:    envDuration("POLL_INTERVAL", 5*time.Second),
		cookDelay:    time.Duration(envInt("COOK_DELAY_MS", 3000)) * time.Millisecond,
		queueName:    env("RESTAURANT_QUEUE", "restaurant-sample.orders"),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
