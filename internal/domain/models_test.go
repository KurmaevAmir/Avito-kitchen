package domain

import "testing"

func TestOrderStatusTransitions(t *testing.T) {
	cases := []struct {
		from, to OrderStatus
		want     bool
	}{
		{OrderStatusCreated, OrderStatusAccepted, true},
		{OrderStatusCreated, OrderStatusCancelled, true},
		{OrderStatusCreated, OrderStatusReady, false},
		{OrderStatusAccepted, OrderStatusCooking, true},
		{OrderStatusAccepted, OrderStatusDelivered, false},
		{OrderStatusCooking, OrderStatusReady, true},
		{OrderStatusReady, OrderStatusDelivered, true},
		{OrderStatusReady, OrderStatusCancelled, false},
		{OrderStatusDelivered, OrderStatusCancelled, false},
		{OrderStatusCancelled, OrderStatusAccepted, false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.want {
			t.Errorf("переход %s -> %s: получили %v, ожидали %v", c.from, c.to, got, c.want)
		}
	}
}

func TestOrderStatusValid(t *testing.T) {
	if OrderStatus("bogus").Valid() {
		t.Error("несуществующий статус 'bogus' не должен быть валидным")
	}
	if !OrderStatusCreated.Valid() {
		t.Error("статус 'created' должен быть валидным")
	}
}
