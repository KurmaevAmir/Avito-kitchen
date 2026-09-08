package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
	"github.com/kurmaevamir/avito-kitchen/internal/repository"
)

// helper — заготовка валидного input для CreateOrder.
func validCreateOrderInput() CreateOrderInput {
	return CreateOrderInput{
		UserID:          "u",
		RestaurantID:    uuid.New(),
		DeliveryAddress: "addr",
		IdempotencyKey:  "k",
		Items: []struct {
			MenuItemID uuid.UUID
			Quantity   int
		}{{MenuItemID: uuid.New(), Quantity: 1}},
	}
}

func TestCreateOrderValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(in *CreateOrderInput)
	}{
		{"empty user_id", func(in *CreateOrderInput) { in.UserID = "" }},
		{"empty idempotency_key", func(in *CreateOrderInput) { in.IdempotencyKey = "" }},
		{"empty delivery", func(in *CreateOrderInput) { in.DeliveryAddress = "" }},
		{"empty items", func(in *CreateOrderInput) { in.Items = nil }},
		{"zero quantity", func(in *CreateOrderInput) { in.Items[0].Quantity = 0 }},
		{"negative quantity", func(in *CreateOrderInput) { in.Items[0].Quantity = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &repoStub{}
			pub := &pubStub{}
			s := New(repo, Options{Publisher: pub})
			in := validCreateOrderInput()
			tc.mut(&in)
			_, _, err := s.CreateOrder(context.Background(), in)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("ожидали ErrValidation, получили %v", err)
			}
			if repo.callsCreateOrder != 0 {
				t.Fatalf("repo.CreateOrder не должен быть вызван на невалидном входе")
			}
			if pub.createdCount != 0 {
				t.Fatalf("событие не должно публиковаться на невалидном входе")
			}
		})
	}
}

func TestCreateOrderPublishesOnlyForFreshOrder(t *testing.T) {
	freshID := uuid.New()
	replayID := uuid.New()

	t.Run("fresh → publish", func(t *testing.T) {
		repo := &repoStub{
			createOrderFn: func(_ context.Context, in repository.CreateOrderInput) (domain.Order, bool, error) {
				return domain.Order{ID: freshID, UserID: in.UserID, RestaurantID: in.RestaurantID}, false, nil
			},
		}
		pub := &pubStub{}
		s := New(repo, Options{Publisher: pub})
		_, existed, err := s.CreateOrder(context.Background(), validCreateOrderInput())
		if err != nil {
			t.Fatal(err)
		}
		if existed {
			t.Fatal("existed=true на fresh")
		}
		if pub.createdCount != 1 || pub.lastCreatedOrderID != freshID {
			t.Fatalf("ожидали 1 publish orders.created, получили %d", pub.createdCount)
		}
	})

	t.Run("idempotent replay → no publish", func(t *testing.T) {
		repo := &repoStub{
			createOrderFn: func(_ context.Context, in repository.CreateOrderInput) (domain.Order, bool, error) {
				return domain.Order{ID: replayID, UserID: in.UserID, RestaurantID: in.RestaurantID}, true, nil
			},
		}
		pub := &pubStub{}
		s := New(repo, Options{Publisher: pub})
		_, existed, err := s.CreateOrder(context.Background(), validCreateOrderInput())
		if err != nil {
			t.Fatal(err)
		}
		if !existed {
			t.Fatal("existed=false на идемпотентный повтор")
		}
		if pub.createdCount != 0 {
			t.Fatalf("на идемпотентный повтор событие не должно публиковаться, получили %d", pub.createdCount)
		}
	})
}

func TestCreateOrderIdempotencyMismatch(t *testing.T) {
	repo := &repoStub{
		createOrderFn: func(context.Context, repository.CreateOrderInput) (domain.Order, bool, error) {
			return domain.Order{}, false, domain.ErrIdempotencyMismatch
		},
	}
	pub := &pubStub{}
	s := New(repo, Options{Publisher: pub})
	_, _, err := s.CreateOrder(context.Background(), validCreateOrderInput())
	if !errors.Is(err, domain.ErrIdempotencyMismatch) {
		t.Fatalf("ожидали ErrIdempotencyMismatch, получили %v", err)
	}
	if pub.createdCount != 0 {
		t.Fatal("не должно быть publish при ErrIdempotencyMismatch")
	}
}

func TestUpdateOrderStatusPublishesOnlyWhenChanged(t *testing.T) {
	t.Run("changed → publish", func(t *testing.T) {
		repo := &repoStub{
			updateStatusFn: func(_ context.Context, rID, oID uuid.UUID, next domain.OrderStatus, _ string) (domain.Order, bool, error) {
				return domain.Order{ID: oID, RestaurantID: rID, Status: next}, true, nil
			},
		}
		pub := &pubStub{}
		s := New(repo, Options{Publisher: pub})
		_, err := s.UpdateOrderStatus(context.Background(), uuid.New(), uuid.New(), domain.OrderStatusAccepted, "ok")
		if err != nil {
			t.Fatal(err)
		}
		if pub.statusChangedCount != 1 {
			t.Fatalf("ожидали 1 publish, получили %d", pub.statusChangedCount)
		}
	})

	t.Run("no-op → no publish (защита от event-loop)", func(t *testing.T) {
		repo := &repoStub{
			updateStatusFn: func(_ context.Context, rID, oID uuid.UUID, next domain.OrderStatus, _ string) (domain.Order, bool, error) {
				return domain.Order{ID: oID, RestaurantID: rID, Status: next}, false, nil
			},
		}
		pub := &pubStub{}
		s := New(repo, Options{Publisher: pub})
		_, err := s.UpdateOrderStatus(context.Background(), uuid.New(), uuid.New(), domain.OrderStatusAccepted, "ok")
		if err != nil {
			t.Fatal(err)
		}
		if pub.statusChangedCount != 0 {
			t.Fatalf("no-op не должен публиковать событие, получили %d", pub.statusChangedCount)
		}
	})
}

func TestUpdateOrderStatusRejectsCreatedTarget(t *testing.T) {
	repo := &repoStub{}
	s := New(repo, Options{})
	_, err := s.UpdateOrderStatus(context.Background(), uuid.New(), uuid.New(), domain.OrderStatusCreated, "")
	if !errors.Is(err, domain.ErrForbiddenTransition) {
		t.Fatalf("ожидали ErrForbiddenTransition, получили %v", err)
	}
	if repo.callsUpdateStatus != 0 {
		t.Fatal("repo не должен вызываться для запрещённого target")
	}
}

func TestCancelOrderAllowedStatuses(t *testing.T) {
	cases := []struct {
		status domain.OrderStatus
		allow  bool
	}{
		{domain.OrderStatusCreated, true},
		{domain.OrderStatusAccepted, true},
		{domain.OrderStatusCooking, false},
		{domain.OrderStatusReady, false},
		{domain.OrderStatusDelivered, false},
		{domain.OrderStatusCancelled, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			repo := &repoStub{
				getOrderFn: func(_ context.Context, id uuid.UUID) (domain.Order, error) {
					return domain.Order{ID: id, UserID: "u", Status: tc.status, RestaurantID: uuid.New()}, nil
				},
			}
			pub := &pubStub{}
			s := New(repo, Options{Publisher: pub})
			_, err := s.CancelOrder(context.Background(), "u", uuid.New(), "")
			if tc.allow {
				if err != nil {
					t.Fatalf("ожидали успех, получили %v", err)
				}
			} else {
				if !errors.Is(err, domain.ErrForbiddenTransition) {
					t.Fatalf("ожидали ErrForbiddenTransition, получили %v", err)
				}
			}
		})
	}
}

func TestCancelOrderForeignOrderIsNotFound(t *testing.T) {
	repo := &repoStub{
		getOrderFn: func(_ context.Context, id uuid.UUID) (domain.Order, error) {
			return domain.Order{ID: id, UserID: "someone-else", Status: domain.OrderStatusCreated}, nil
		},
	}
	s := New(repo, Options{})
	_, err := s.CancelOrder(context.Background(), "u", uuid.New(), "")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("чужой заказ должен маскироваться в ErrNotFound, получили %v", err)
	}
}

func TestAuthenticatePartnerEmptyToken(t *testing.T) {
	s := New(&repoStub{}, Options{})
	_, err := s.AuthenticatePartner(context.Background(), "")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("пустой bearer → ErrUnauthorized, получили %v", err)
	}
}

func TestAuthenticatePartnerWrongToken(t *testing.T) {
	repo := &repoStub{restaurantHashErr: domain.ErrNotFound}
	s := New(repo, Options{})
	_, err := s.AuthenticatePartner(context.Background(), "wrong")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("неверный bearer → ErrUnauthorized, получили %v", err)
	}
}
