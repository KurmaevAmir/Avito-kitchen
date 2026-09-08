package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

// RabbitPublisher — реализация publisher'а поверх RabbitMQ topic-exchange.
// Держит одно соединение и один канал; при обрыве соединения фоновая
// горутина переподключается. Publish на «пока переподключаемся»
// возвращает ошибку в лог (см. publish); в prod к этому должен идти
// transactional outbox (см. ADR-C7 в architecture-plan.md).
type RabbitPublisher struct {
	url string
	log *slog.Logger

	mu   sync.RWMutex
	conn *amqp.Connection
	ch   *amqp.Channel
	done chan struct{}
}

// NewRabbitPublisher подключается к RabbitMQ, объявляет topic-exchange
// и запускает supervisor-горутину, которая переподключается при обрыве
// соединения.
func NewRabbitPublisher(url string, log *slog.Logger) (*RabbitPublisher, error) {
	p := &RabbitPublisher{url: url, log: log, done: make(chan struct{})}
	if err := p.connect(); err != nil {
		return nil, err
	}
	go p.supervise()
	return p, nil
}

// connect устанавливает conn+channel и объявляет exchange.
func (p *RabbitPublisher) connect() error {
	conn, err := DialWithRetry(p.url, 30*time.Second, p.log)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.ExchangeDeclare(ExchangeOrders, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	p.mu.Lock()
	p.conn = conn
	p.ch = ch
	p.mu.Unlock()
	return nil
}

// supervise слушает NotifyClose и переподключается на обрыв. Выходит,
// когда Close закрыл done.
func (p *RabbitPublisher) supervise() {
	for {
		p.mu.RLock()
		conn := p.conn
		p.mu.RUnlock()
		if conn == nil {
			return
		}
		closeErr := make(chan *amqp.Error, 1)
		conn.NotifyClose(closeErr)
		select {
		case <-p.done:
			return
		case err := <-closeErr:
			if err == nil {
				return // штатное закрытие
			}
			p.log.Warn("RabbitMQ соединение оборвано, переподключаемся", "err", err.Error())
			for {
				if reconnErr := p.connect(); reconnErr == nil {
					p.log.Info("RabbitMQ переподключён")
					break
				} else {
					p.log.Warn("не удалось переподключиться к RabbitMQ", "err", reconnErr.Error())
					select {
					case <-p.done:
						return
					case <-time.After(2 * time.Second):
					}
				}
			}
		}
	}
}

// Close останавливает supervisor и освобождает AMQP-соединение.
func (p *RabbitPublisher) Close() error {
	close(p.done)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// PublishOrderCreated публикует событие о новом заказе.
func (p *RabbitPublisher) PublishOrderCreated(ctx context.Context, order domain.Order) {
	evt := OrderCreatedEvent{
		EventID:      uuid.New(),
		OccurredAt:   time.Now().UTC(),
		OrderID:      order.ID,
		RestaurantID: order.RestaurantID,
		UserID:       order.UserID,
		TotalPrice:   order.TotalPrice,
		Items:        eventItems(order.Items),
	}
	p.publish(ctx, RoutingCreatedFor(order.RestaurantID.String()), evt)
}

// PublishOrderStatusChanged публикует событие о смене статуса заказа.
func (p *RabbitPublisher) PublishOrderStatusChanged(ctx context.Context, order domain.Order, comment string) {
	evt := OrderStatusChangedEvent{
		EventID:      uuid.New(),
		OccurredAt:   time.Now().UTC(),
		OrderID:      order.ID,
		RestaurantID: order.RestaurantID,
		NewStatus:    order.Status,
		Comment:      comment,
	}
	p.publish(ctx, RoutingStatusFor(order.RestaurantID.String(), string(order.Status)), evt)
}

func (p *RabbitPublisher) publish(ctx context.Context, routingKey string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		p.log.Error("не удалось сериализовать событие", "routing_key", routingKey, "err", err.Error())
		return
	}
	p.mu.RLock()
	ch := p.ch
	p.mu.RUnlock()
	if ch == nil {
		p.log.Error("канал RabbitMQ закрыт, событие потеряно",
			"routing_key", routingKey, "note", "в prod здесь работал бы outbox — см. ADR-C7")
		return
	}
	// Ограничиваем время публикации, чтобы медленный брокер не подвешивал
	// хендлер, вернувший ответ клиенту.
	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err = ch.PublishWithContext(pubCtx, ExchangeOrders, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
	})
	if err != nil {
		p.log.Error("не удалось опубликовать событие", "routing_key", routingKey, "err", err.Error())
		return
	}
	p.log.Debug("событие опубликовано", "routing_key", routingKey, "bytes", len(body))
}

// DialWithRetry подключается к RabbitMQ с ретраями раз в 2 секунды в
// пределах timeout. Брокер в compose стартует не мгновенно.
func DialWithRetry(url string, timeout time.Duration, log *slog.Logger) (*amqp.Connection, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		log.Info("RabbitMQ пока не готов, повторяем попытку", "err", err.Error())
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
