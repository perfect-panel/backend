package orderLogic

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublishOrderEventsDeliversDurableOutboxThenMarksPublished(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:publish-order-events?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&order.Event{}); err != nil {
		t.Fatalf("migrate event: %v", err)
	}
	event := &order.Event{OrderID: 1, OrderNo: "outbox-order", EventType: "order.created", Payload: `{}`}
	if err := db.Create(event).Error; err != nil {
		t.Fatalf("seed event: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	pubsub := redisClient.Subscribe(context.Background(), orderstream.Channel(event.OrderNo))
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	logic := NewPublishOrderEventsLogic(&svc.ServiceContext{Store: repository.NewGormStore(db, redisClient), Redis: redisClient})
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("publish outbox: %v", err)
	}
	select {
	case message := <-pubsub.Channel():
		if message.Payload != strconv.FormatInt(event.ID, 10) {
			t.Fatalf("published payload = %q, want event id %d", message.Payload, event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive published order event")
	}
	var latest order.Event
	if err := db.First(&latest, event.ID).Error; err != nil {
		t.Fatalf("reload event: %v", err)
	}
	if latest.PublishedAt == nil {
		t.Fatal("published event did not receive published_at")
	}
}

func TestCleanupOrderEventsKeepsUnpublishedRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:cleanup-order-events?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&order.Event{}); err != nil {
		t.Fatalf("migrate event: %v", err)
	}
	old := time.Now().Add(-orderEventRetention - time.Hour)
	publishedAt := old
	published := &order.Event{OrderID: 1, OrderNo: "old-published", EventType: "order.created", Payload: `{}`, CreatedAt: old, PublishedAt: &publishedAt}
	unpublished := &order.Event{OrderID: 2, OrderNo: "old-unpublished", EventType: "order.created", Payload: `{}`, CreatedAt: old}
	if err := db.Create(published).Error; err != nil {
		t.Fatalf("seed published event: %v", err)
	}
	if err := db.Create(unpublished).Error; err != nil {
		t.Fatalf("seed unpublished event: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	logic := NewCleanupOrderEventsLogic(&svc.ServiceContext{Store: repository.NewGormStore(db, redisClient)})
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("cleanup events: %v", err)
	}
	var events []order.Event
	if err := db.Find(&events).Error; err != nil {
		t.Fatalf("reload events: %v", err)
	}
	if len(events) != 1 || events[0].OrderNo != unpublished.OrderNo {
		t.Fatalf("remaining events = %#v, want only unpublished event", events)
	}
}
