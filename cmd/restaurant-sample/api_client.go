package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// order — минимальная проекция заказа для нужд sample-сервиса.
type order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type listResp struct {
	Items []order `json:"items"`
}

// apiClient — HTTP-клиент partner-API главного сервиса. Автоматически
// подставляет Authorization: Bearer <api_key> в каждый запрос.
type apiClient struct {
	http *http.Client
	cfg  config
	log  *slog.Logger
}

func newAPIClient(h *http.Client, cfg config, log *slog.Logger) *apiClient {
	return &apiClient{http: h, cfg: cfg, log: log}
}

// waitReady опрашивает /readyz до 60 секунд. Нужно на холодном старте
// compose, когда api-контейнер ещё поднимается.
func (a *apiClient) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.apiBase+"/readyz", nil)
		resp, err := a.http.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				a.log.Info("API готов")
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("таймаут ожидания готовности API")
}

// fetchByStatus возвращает заказы заведения с заданным статусом.
// На любых ошибках возвращает пустой слайс и логгирует — caller
// продолжает работу.
func (a *apiClient) fetchByStatus(ctx context.Context, status string) []order {
	url := fmt.Sprintf("%s/v1/partner/orders?status=%s&limit=50", a.cfg.apiBase, status)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	a.setAuth(req)
	resp, err := a.http.Do(req)
	if err != nil {
		a.log.Warn("не удалось получить заказы", "status", status, "err", err.Error())
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		a.log.Warn("получение заказов вернуло нештатный код",
			"status", status, "http", resp.StatusCode, "body", string(body))
		return nil
	}
	var out listResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		a.log.Warn("не удалось декодировать список заказов", "err", err.Error())
		return nil
	}
	return out.Items
}

// patchResult — итог PATCH со стороны consumer'а. Разделяет retryable
// сбои (сетевые/5xx) и постоянные (4xx), чтобы AMQP-обработчик мог
// корректно nack/requeue vs nack/drop.
type patchResult int

const (
	patchOK             patchResult = iota // 2xx
	patchRetryable                         // сетевой сбой или 5xx — nack с requeue
	patchClientRejected                    // 4xx (кроме 409) — сообщение битое, nack без requeue
	patchNoChange                          // 409 forbidden_transition — заказ уже дальше, no-op
)

// patchStatus меняет статус заказа и возвращает patchResult.
func (a *apiClient) patchStatus(ctx context.Context, id, status, comment string) patchResult {
	body, _ := json.Marshal(map[string]string{"status": status, "comment": comment})
	url := fmt.Sprintf("%s/v1/partner/orders/%s/status", a.cfg.apiBase, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	a.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		a.log.Warn("не удалось обновить статус заказа", "id", id, "err", err.Error())
		return patchRetryable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 300 {
		a.log.Info("заказ продвинут", "id", id, "to", status)
		return patchOK
	}
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		// Заказ уже в целевом статусе или дальше по цепочке — событие
		// пришло дважды, тихо принимаем.
		a.log.Info("статус не изменён (409)", "id", id, "to", status, "body", string(respBody))
		return patchNoChange
	}
	if resp.StatusCode >= 500 {
		a.log.Warn("обновление статуса: 5xx", "id", id, "http", resp.StatusCode, "body", string(respBody))
		return patchRetryable
	}
	a.log.Warn("обновление статуса: 4xx (сообщение будет отброшено)",
		"id", id, "http", resp.StatusCode, "body", string(respBody))
	return patchClientRejected
}

func (a *apiClient) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.cfg.restKey)
}
