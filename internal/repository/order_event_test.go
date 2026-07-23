package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrderRepoWritesDurableEventsWithStateTransitions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:order-events?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&order.Order{}, &order.Event{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	repo := newOrderRepo(db, redisClient)
	created := &order.Order{OrderNo: "v2-order-1", Status: 1}
	if err := repo.Insert(context.Background(), created); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if created.StateVersion != 1 {
		t.Fatalf("created state version = %d, want 1", created.StateVersion)
	}

	updated, err := repo.UpdateOrderStatusFrom(context.Background(), created.OrderNo, 1, 2)
	if err != nil || !updated {
		t.Fatalf("mark paid = (%v, %v), want (true, nil)", updated, err)
	}
	updated, err = repo.UpdateOrderStatusFrom(context.Background(), created.OrderNo, 2, 5)
	if err != nil || !updated {
		t.Fatalf("mark finished = (%v, %v), want (true, nil)", updated, err)
	}

	var events []order.Event
	if err := db.Order("id ASC").Find(&events).Error; err != nil {
		t.Fatalf("find events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if got, want := []string{events[0].EventType, events[1].EventType, events[2].EventType}, []string{orderEventCreated, orderEventPaymentPaid, orderEventFulfilled}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	var payload orderEventPayload
	if err := json.Unmarshal([]byte(events[2].Payload), &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if payload.StateVersion != 3 || payload.PaymentStatus != "paid" || payload.FulfillmentStatus != "finished" {
		t.Fatalf("finished payload = %#v", payload)
	}

	latest, err := repo.FindOneByOrderNo(context.Background(), created.OrderNo)
	if err != nil {
		t.Fatalf("find latest order: %v", err)
	}
	if latest.StateVersion != 3 || latest.Status != 5 {
		t.Fatalf("latest state = (version %d, status %d), want (3, 5)", latest.StateVersion, latest.Status)
	}
}

func TestOrderEventRepoReplaysByOrderAndMarksPublished(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:order-event-replay?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&order.Event{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	for _, event := range []*order.Event{
		{OrderID: 1, OrderNo: "order-a", EventType: orderEventCreated, Payload: `{}`},
		{OrderID: 1, OrderNo: "order-a", EventType: orderEventPaymentPaid, Payload: `{}`},
		{OrderID: 2, OrderNo: "order-b", EventType: orderEventCreated, Payload: `{}`},
	} {
		if err := db.Create(event).Error; err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	repo := newOrderEventRepo(db)
	events, err := repo.ListAfter(context.Background(), "order-a", 1, 100)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(events) != 1 || events[0].EventType != orderEventPaymentPaid {
		t.Fatalf("replay events = %#v", events)
	}
	earliestID, err := repo.EarliestID(context.Background(), "order-a")
	if err != nil || earliestID != 1 {
		t.Fatalf("earliest event = (%d, %v), want (1, nil)", earliestID, err)
	}
	unpublished, err := repo.ListUnpublished(context.Background(), 10)
	if err != nil || len(unpublished) != 3 {
		t.Fatalf("unpublished = (%d, %v), want (3, nil)", len(unpublished), err)
	}
	marked, err := repo.MarkPublished(context.Background(), unpublished[0].ID, unpublished[0].CreatedAt)
	if err != nil || !marked {
		t.Fatalf("mark published = (%v, %v), want (true, nil)", marked, err)
	}
	marked, err = repo.MarkPublished(context.Background(), unpublished[0].ID, unpublished[0].CreatedAt)
	if err != nil || marked {
		t.Fatalf("repeat mark published = (%v, %v), want (false, nil)", marked, err)
	}
	deleted, err := repo.DeletePublishedBefore(context.Background(), time.Now().Add(time.Minute))
	if err != nil || deleted != 1 {
		t.Fatalf("delete published = (%d, %v), want (1, nil)", deleted, err)
	}
}

func TestOrderStatusTransitionRollsBackWhenOutboxWriteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:order-event-rollback?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Deliberately omit order_event. The state write and event write must be
	// atomic, so a missing outbox table cannot leave a paid order with no event.
	if err := db.AutoMigrate(&order.Order{}); err != nil {
		t.Fatalf("migrate order: %v", err)
	}
	pending := &order.Order{OrderNo: "rollback-order", Status: 1, StateVersion: 1}
	if err := db.Create(pending).Error; err != nil {
		t.Fatalf("seed pending order: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	repo := newOrderRepo(db, redisClient)
	updated, err := repo.UpdateOrderStatusFrom(context.Background(), pending.OrderNo, 1, 2)
	if err == nil || updated {
		t.Fatalf("transition with missing event table = (%v, %v), want (false, error)", updated, err)
	}
	var latest order.Order
	if err := db.Where("order_no = ?", pending.OrderNo).First(&latest).Error; err != nil {
		t.Fatalf("reload pending order: %v", err)
	}
	if latest.Status != 1 || latest.StateVersion != 1 {
		t.Fatalf("rolled back state = (status %d, version %d), want (1, 1)", latest.Status, latest.StateVersion)
	}
}

func TestOrderRepoUpdateRejectsStateMutationOutsideTransition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:order-update-state-guard?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&order.Order{}, &order.Event{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	repo := newOrderRepo(db, redisClient)
	pending := &order.Order{OrderNo: "guard-order", Status: 1}
	if err := repo.Insert(context.Background(), pending); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	pending.Status = 2
	if err := repo.Update(context.Background(), pending); err == nil {
		t.Fatal("generic update must not change an order state")
	}
	var latest order.Order
	if err := db.Where("order_no = ?", pending.OrderNo).First(&latest).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if latest.Status != 1 || latest.StateVersion != 1 {
		t.Fatalf("generic update changed state to (%d, %d)", latest.Status, latest.StateVersion)
	}
	var events []order.Event
	if err := db.Where("order_no = ?", pending.OrderNo).Find(&events).Error; err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want only creation event", len(events))
	}
}
