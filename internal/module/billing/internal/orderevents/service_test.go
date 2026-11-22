package orderevents

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/redis/go-redis/v9"
)

// recordingBroadcaster records the ids of the events it broadcast.
type recordingBroadcaster struct{ ids []int64 }

var _ Broadcaster = (*recordingBroadcaster)(nil)

func (b *recordingBroadcaster) Broadcast(_ context.Context, event *order.Event) error {
	b.ids = append(b.ids, event.ID)
	return nil
}

// seedEvent stores an order event as the order repository writes it.
func seedEvent(t *testing.T, h *billingtest.Harness, event *order.Event) *order.Event {
	t.Helper()
	event.EventType, event.Payload = "order.created", `{}`
	if err := h.DB.Create(event).Error; err != nil {
		t.Fatalf("seed event of %s: %v", event.OrderNo, err)
	}
	return event
}

func unpublished(t *testing.T, h *billingtest.Harness) int {
	t.Helper()
	events, err := h.Store.OrderEvent().ListUnpublished(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}

// Publishing wakes the order's streams with the event's id, then marks the
// event published.
func TestPublishBroadcastsTheOutboxThenMarksItPublished(t *testing.T) {
	h := billingtest.New(t)
	event := seedEvent(t, h, &order.Event{OrderID: 1, OrderNo: "outbox-order"})
	pubsub := h.Redis.Subscribe(context.Background(), order.EventChannel(event.OrderNo))
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := NewService(h.Store.OrderEvent(), RedisBroadcaster{Client: h.Redis}).Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case message := <-pubsub.Channel():
		if message.Payload != strconv.FormatInt(event.ID, 10) {
			t.Fatalf("published payload = %q, want event id %d", message.Payload, event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive the published order event")
	}
	if stored := h.Events(event.OrderNo); len(stored) != 1 || stored[0].PublishedAt == nil {
		t.Fatalf("events = %+v, want the event marked published", stored)
	}
}

// Design: when publishing fails the next run publishes the event, and a
// repeated publication never changes the order.
func TestPublishRepublishesAfterAFailure(t *testing.T) {
	h := billingtest.New(t)
	// The broadcast goes to a Redis of its own, which the test stops.
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = redisClient.Close() })
	paid := h.Order(&order.Order{OrderNo: "republish", Status: order.StatusPending})
	if _, err := h.Store.Order().MarkOrderPaid(context.Background(), paid.OrderNo, "trade-1"); err != nil {
		t.Fatal(err)
	}
	svc := NewService(h.Store.OrderEvent(), RedisBroadcaster{Client: redisClient})

	redisServer.Close()
	if err := svc.Publish(context.Background()); err == nil {
		t.Fatal("publishing without Redis succeeded")
	}
	if unpublished(t, h) != 1 {
		t.Fatal("the failed publication marked the event published")
	}
	if err := redisServer.Restart(); err != nil {
		t.Fatal(err)
	}
	pubsub := redisClient.Subscribe(context.Background(), order.EventChannel(paid.OrderNo))
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for range 2 {
		// A crash between publishing and marking publishes the event again.
		if err := h.DB.Model(&order.Event{}).Where("order_no = ?", paid.OrderNo).Update("published_at", nil).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.Publish(context.Background()); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		select {
		case <-pubsub.Channel():
		case <-time.After(time.Second):
			t.Fatal("the event was not published")
		}
	}
	if unpublished(t, h) != 0 {
		t.Fatal("the published event stayed in the outbox")
	}
	if latest := h.ReloadOrder(paid.OrderNo); latest.Status != order.StatusPaid || latest.StateVersion != 2 {
		t.Fatalf("order = status %d version %d, want the paid state unchanged", latest.Status, latest.StateVersion)
	}
}

// One run publishes the oldest batch in id order; the next run publishes the
// rest.
func TestPublishDrainsTheOutboxInBatches(t *testing.T) {
	h := billingtest.New(t)
	events := make([]*order.Event, publishBatch+1)
	for i := range events {
		events[i] = &order.Event{OrderID: int64(i + 1), OrderNo: fmt.Sprintf("batch-%d", i), EventType: "order.created", Payload: `{}`}
	}
	if err := h.DB.CreateInBatches(events, 100).Error; err != nil {
		t.Fatalf("seed events: %v", err)
	}
	broadcaster := &recordingBroadcaster{}
	svc := NewService(h.Store.OrderEvent(), broadcaster)

	if err := svc.Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(broadcaster.ids) != publishBatch || !slices.IsSorted(broadcaster.ids) || broadcaster.ids[0] != events[0].ID {
		t.Fatalf("the first run broadcast %d events, want the oldest %d in id order", len(broadcaster.ids), publishBatch)
	}
	if unpublished(t, h) != 1 {
		t.Fatal("the first run published more than its batch")
	}
	if err := svc.Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(broadcaster.ids) != publishBatch+1 || broadcaster.ids[publishBatch] != events[publishBatch].ID {
		t.Fatalf("the second run broadcast %d events, want only the remaining event %d", len(broadcaster.ids)-publishBatch, events[publishBatch].ID)
	}
	if unpublished(t, h) != 0 {
		t.Fatal("an event stayed unpublished")
	}
}

// The cleanup deletes only the published events created before the cutoff:
// an unpublished event outlives any retention.
func TestCleanupKeepsUnpublishedAndRecentEvents(t *testing.T) {
	h := billingtest.New(t)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	old, recent := cutoff.Add(-time.Hour), cutoff.Add(time.Hour)
	seedEvent(t, h, &order.Event{OrderID: 1, OrderNo: "old-published", CreatedAt: old, PublishedAt: &old})
	seedEvent(t, h, &order.Event{OrderID: 2, OrderNo: "old-unpublished", CreatedAt: old})
	seedEvent(t, h, &order.Event{OrderID: 3, OrderNo: "recent-published", CreatedAt: recent, PublishedAt: &recent})

	deleted, err := NewService(h.Store.OrderEvent(), &recordingBroadcaster{}).Cleanup(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	var remaining []string
	if err := h.DB.Model(&order.Event{}).Order("id ASC").Pluck("order_no", &remaining).Error; err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || !slices.Equal(remaining, []string{"old-unpublished", "recent-published"}) {
		t.Fatalf("deleted %d, remaining %v; want only the old published event deleted", deleted, remaining)
	}
}
