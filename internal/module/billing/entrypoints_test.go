package billing_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"gorm.io/gorm"
)

// The task handlers and the notify middleware reach billing's order, event
// and payment tables only through these facade methods.

// A committed order change reaches the order's streams through the facade's
// outbox publication, and the cleanup prunes the published event once it is
// older than the cutoff.
func TestOrderEventOutboxThroughTheFacade(t *testing.T) {
	f := newFacade(t)
	ctx := context.Background()
	o := f.h.Order(&order.Order{OrderNo: "outbox-1", Status: order.StatusPending})
	if err := f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPaid, TradeNo: "trade-1"}); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	events := f.h.Events(o.OrderNo)
	if len(events) != 1 {
		t.Fatalf("events = %d, want the payment event", len(events))
	}
	pubsub := f.h.Redis.Subscribe(ctx, order.EventChannel(o.OrderNo))
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := f.svc.PublishOrderEvents(ctx); err != nil {
		t.Fatalf("PublishOrderEvents: %v", err)
	}
	select {
	case message := <-pubsub.Channel():
		if message.Payload != strconv.FormatInt(events[0].ID, 10) {
			t.Fatalf("published payload = %q, want event id %d", message.Payload, events[0].ID)
		}
	case <-time.After(time.Second):
		t.Fatal("the stream was not woken")
	}
	if published := f.h.Events(o.OrderNo); published[0].PublishedAt == nil {
		t.Fatal("the event was not marked published")
	}

	if deleted, err := f.svc.CleanupOrderEvents(ctx, time.Now().Add(-time.Hour)); err != nil || deleted != 0 {
		t.Fatalf("cleanup before the event = %d, %v; want nothing deleted", deleted, err)
	}
	if deleted, err := f.svc.CleanupOrderEvents(ctx, time.Now().Add(time.Hour)); err != nil || deleted != 1 {
		t.Fatalf("cleanup after the event = %d, %v; want the published event deleted", deleted, err)
	}
	if remaining := f.h.Events(o.OrderNo); len(remaining) != 0 {
		t.Fatalf("remaining events = %d, want none", len(remaining))
	}
}

// The reconcilers page one status by ascending id.
func TestOrdersByStatusAfterPagesOneStatusByID(t *testing.T) {
	f := newFacade(t)
	ctx := context.Background()
	var pending []int64
	for i, status := range []uint8{order.StatusPending, order.StatusPaid, order.StatusPending, order.StatusPending} {
		o := f.h.Order(&order.Order{OrderNo: "page-" + strconv.Itoa(i), Status: status})
		if status == order.StatusPending {
			pending = append(pending, o.Id)
		}
	}
	ids := func(orders []*order.Order) []int64 {
		result := make([]int64, 0, len(orders))
		for _, o := range orders {
			result = append(result, o.Id)
		}
		return result
	}

	first, err := f.svc.OrdersByStatusAfter(ctx, order.StatusPending, 0, 2)
	if err != nil || !slices.Equal(ids(first), pending[:2]) {
		t.Fatalf("first page = %v, %v; want %v", ids(first), err, pending[:2])
	}
	rest, err := f.svc.OrdersByStatusAfter(ctx, order.StatusPending, pending[1], 2)
	if err != nil || !slices.Equal(ids(rest), pending[2:]) {
		t.Fatalf("second page = %v, %v; want %v", ids(rest), err, pending[2:])
	}
}

// The notify middleware resolves the payment method a callback URL's token
// names; an unknown token fails with the repository's not-found error.
func TestFindPaymentMethodByTokenResolvesTheNotifyToken(t *testing.T) {
	f := newFacade(t)
	method := f.h.Payment("EPay", epayConfig)

	found, err := f.svc.FindPaymentMethodByToken(context.Background(), method.Token)
	if err != nil || found.Id != method.Id || found.Platform != "EPay" {
		t.Fatalf("found = %+v, %v; want payment method %d", found, err, method.Id)
	}
	if _, err := f.svc.FindPaymentMethodByToken(context.Background(), "unknown-token"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown token = %v, want record not found", err)
	}
}
