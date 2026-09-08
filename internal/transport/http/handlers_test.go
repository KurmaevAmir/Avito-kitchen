package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
	"github.com/kurmaevamir/avito-kitchen/internal/service"
)

// stubPinger — заглушка Deps.Pinger для теста /readyz.
type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

// repoStub — минимальный мок service.Repository для integration-тестов
// хендлеров. Никакой БД, никакого RabbitMQ.
type repoStub struct {
	restaurant       domain.Restaurant
	restaurantByHash domain.Restaurant
	restaurantErr    error
}

func (s *repoStub) Ping(context.Context) error { return nil }
func (s *repoStub) ListRestaurants(context.Context, domain.OpenFilter, int, int) ([]domain.Restaurant, error) {
	return []domain.Restaurant{s.restaurant}, nil
}
func (s *repoStub) GetRestaurant(context.Context, uuid.UUID) (domain.Restaurant, error) {
	return s.restaurant, s.restaurantErr
}
func (s *repoStub) GetRestaurantByAPIKeyHash(context.Context, string) (domain.Restaurant, error) {
	if s.restaurantByHash.ID == (uuid.UUID{}) {
		return domain.Restaurant{}, domain.ErrNotFound
	}
	return s.restaurantByHash, nil
}
func (s *repoStub) ListAvailableMenu(context.Context, uuid.UUID) ([]domain.MenuItem, error) {
	return nil, nil
}
func (s *repoStub) ListAllMenu(context.Context, uuid.UUID) ([]domain.MenuItem, error) {
	return nil, nil
}
func (s *repoStub) CreateMenuItem(_ context.Context, m domain.MenuItem) (domain.MenuItem, error) {
	return m, nil
}
func (s *repoStub) PatchMenuItem(context.Context, uuid.UUID, uuid.UUID, *int64, *bool) (domain.MenuItem, error) {
	return domain.MenuItem{}, nil
}
func (s *repoStub) SoftDeleteMenuItem(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *repoStub) CreateOrder(context.Context, repository.CreateOrderInput) (domain.Order, bool, error) {
	return domain.Order{}, false, nil
}
func (s *repoStub) GetOrder(context.Context, uuid.UUID) (domain.Order, error) {
	return domain.Order{}, nil
}
func (s *repoStub) ListOrderStatusHistory(context.Context, uuid.UUID) ([]domain.OrderStatusEvent, error) {
	return nil, nil
}
func (s *repoStub) ListUserOrders(context.Context, string, int, int) ([]domain.Order, error) {
	return nil, nil
}
func (s *repoStub) ListRestaurantOrders(context.Context, uuid.UUID, string, int) ([]domain.Order, error) {
	return nil, nil
}
func (s *repoStub) UpdateOrderStatus(context.Context, uuid.UUID, uuid.UUID, domain.OrderStatus, string) (domain.Order, bool, error) {
	return domain.Order{}, true, nil
}

func testRouter(_ *testing.T, repo service.Repository) http.Handler {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := service.New(repo, service.Options{})
	return NewRouter(Deps{
		Service:     svc,
		Logger:      log,
		Pinger:      stubPinger{},
		OpenAPIPath: os.DevNull,
	})
}

func TestHealthz(t *testing.T) {
	rec := do(t, testRouter(t, &repoStub{}), http.MethodGet, "/healthz", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d", rec.Code)
	}
}

func TestReadyzDBFailureMasksMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := service.New(&repoStub{}, service.Options{})
	r := NewRouter(Deps{
		Service: svc, Logger: log,
		Pinger:      stubPinger{err: errString("host=pg-primary port=5432 database=leaked")},
		OpenAPIPath: os.DevNull,
	})
	rec := do(t, r, http.MethodGet, "/readyz", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ожидали 503, получили %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "pg-primary") || strings.Contains(body, "5432") {
		t.Fatalf("readyz не должен раскрывать pg-ошибку клиенту, тело: %s", body)
	}
}

func TestGetRestaurantBadUUID(t *testing.T) {
	rec := do(t, testRouter(t, &repoStub{}), http.MethodGet, "/v1/restaurants/not-a-uuid", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400 на битый UUID, получили %d", rec.Code)
	}
}

func TestListUserOrdersMissingHeader(t *testing.T) {
	// service.ListUserOrders валидирует пустой userID и возвращает ErrValidation.
	rec := do(t, testRouter(t, &repoStub{}), http.MethodGet, "/v1/orders", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидали 400 без X-User-Id, получили %d", rec.Code)
	}
	var e map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e["code"] != "validation_failed" {
		t.Fatalf("ожидали code=validation_failed, получили %v", e["code"])
	}
}

func TestPartnerAuthUnauthorized(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"missing", nil},
		{"wrong scheme", map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}},
		{"wrong token", map[string]string{"Authorization": "Bearer totally-wrong"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, testRouter(t, &repoStub{}), http.MethodGet, "/v1/partner/menu", nil, tc.headers)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("ожидали 401, получили %d", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Fatalf("ожидали заголовок WWW-Authenticate: Bearer, получили %q",
					rec.Header().Get("WWW-Authenticate"))
			}
			var e map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &e)
			if e["code"] != "unauthorized" {
				t.Fatalf("ожидали code=unauthorized, получили %v", e["code"])
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := service.New(&repoStub{}, service.Options{})
	r := NewRouter(Deps{
		Service: svc, Logger: log, Pinger: stubPinger{},
		OpenAPIPath: os.DevNull,
		CORSOrigins: []string{"http://localhost:3000"},
	})
	rec := do(t, r, http.MethodOptions, "/v1/restaurants", nil, map[string]string{
		"Origin":                        "http://localhost:3000",
		"Access-Control-Request-Method": "GET",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ожидали 204 на preflight, получили %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("ожидали ACAO=http://localhost:3000, получили %q", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-User-Id") {
		t.Fatal("ACAH должен содержать X-User-Id")
	}
}

// helpers

func do(_ *testing.T, h http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type errString string

func (e errString) Error() string { return string(e) }
