package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/kurmaevamir/avito-kitchen/internal/events"
)

// runEventDriven подключается к RabbitMQ, объявляет очередь, привязывает
// её к routing-key своего заведения и в цикле обрабатывает сообщения.
func runEventDriven(ctx context.Context, log *slog.Logger, api *apiClient, cfg config) error {
	conn, err := events.DialWithRetry(cfg.rabbitURL, 30*time.Second, log)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()

	if err := ch.ExchangeDeclare(events.ExchangeOrders, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	q, err := ch.QueueDeclare(cfg.queueName, true, false, false, false, nil)
	if err != nil {
		return err
	}
	// Мультитенантный биндинг: слушаем только события своего заведения.
	// routing key имеет вид orders.<restaurant_id>.created или
	// orders.<restaurant_id>.status.<status>, паттерн orders.<id>.# ловит
	// оба варианта.
	rk := "orders." + cfg.restaurantID + ".#"
	if err := ch.QueueBind(q.Name, rk, events.ExchangeOrders, false, nil); err != nil {
		return err
	}
	if err := ch.Qos(10, 0, false); err != nil {
		return err
	}
	msgs, err := ch.Consume(q.Name, "restaurant-sample", false, false, false, false, nil)
	if err != nil {
		return err
	}
	log.Info("restaurant-sample запущен в event-driven режиме",
		"queue", q.Name, "binding", rk)

	for {
		select {
		case <-ctx.Done():
			log.Info("restaurant-sample останавливается")
			return nil
		case msg, ok := <-msgs:
			if !ok {
				return fmt.Errorf("канал consume закрыт брокером")
			}
			handleMessage(ctx, log, api, cfg, msg)
		}
	}
}

// handleMessage обрабатывает одно AMQP-сообщение и подтверждает/отвергает
// его по итогу вызова partner-API. Retryable-сбои (сетевые, 5xx) идут в
// requeue; 4xx — в drop; успех и no-op — ack.
func handleMessage(ctx context.Context, log *slog.Logger, api *apiClient, cfg config, msg amqp.Delivery) {
	orderID, target, err := decodeAction(msg)
	if err != nil {
		log.Warn("не удалось разобрать событие", "routing_key", msg.RoutingKey, "err", err.Error())
		_ = msg.Nack(false, false)
		return
	}
	if target == "" {
		// Событие не для нас (например, orders.<id>.status.delivered).
		_ = msg.Ack(false)
		return
	}
	if target == "ready" {
		// «Готовим» с уважением к ctx: на SIGTERM выходим сразу, не
		// подвешивая брокер на неподтверждённом сообщении.
		select {
		case <-ctx.Done():
			_ = msg.Nack(false, true) // вернём в очередь, обработает следующий инстанс
			return
		case <-time.After(cfg.cookDelay):
		}
	}
	comment := commentFor(target)
	switch api.patchStatus(ctx, orderID, target, comment) {
	case patchOK, patchNoChange:
		_ = msg.Ack(false)
	case patchRetryable:
		// Возвращаем в очередь. RabbitMQ поставит в конец — простейший backoff.
		_ = msg.Nack(false, true)
	case patchClientRejected:
		_ = msg.Nack(false, false)
	}
}

// decodeAction извлекает id заказа и целевой следующий статус из
// события. Пустой target означает «на это событие мы не реагируем».
func decodeAction(msg amqp.Delivery) (orderID, target string, err error) {
	// Ищем в routing key последний токен, но проще смотреть на суффикс.
	// Ключ полностью «известен на границе» — не парсим URL-подобные штуки.
	switch {
	case endsWith(msg.RoutingKey, ".created"):
		var e events.OrderCreatedEvent
		if err := json.Unmarshal(msg.Body, &e); err != nil {
			return "", "", err
		}
		return e.OrderID.String(), "accepted", nil
	case endsWith(msg.RoutingKey, ".status.accepted"):
		return decodeStatusEvent(msg, "cooking")
	case endsWith(msg.RoutingKey, ".status.cooking"):
		return decodeStatusEvent(msg, "ready")
	case endsWith(msg.RoutingKey, ".status.ready"):
		return decodeStatusEvent(msg, "delivered")
	default:
		return "", "", nil
	}
}

func decodeStatusEvent(msg amqp.Delivery, target string) (string, string, error) {
	var e events.OrderStatusChangedEvent
	if err := json.Unmarshal(msg.Body, &e); err != nil {
		return "", "", err
	}
	return e.OrderID.String(), target, nil
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func commentFor(target string) string {
	switch target {
	case "accepted":
		return "авто-подтверждено демо-кухней (событие)"
	case "cooking":
		return "начали готовить"
	case "ready":
		return "приготовлено и упаковано"
	case "delivered":
		return "передано курьеру"
	}
	return ""
}
