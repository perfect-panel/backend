package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
)

type fakeOutbox struct {
	rows      []*outbox.Event
	published []int64
	markErr   error
}

func (f *fakeOutbox) Append(ctx context.Context, topic, eventKey, payload string) error {
	panic("not used")
}

func (f *fakeOutbox) ListUnpublished(ctx context.Context, limit int) ([]*outbox.Event, error) {
	if limit > len(f.rows) {
		limit = len(f.rows)
	}
	return f.rows[:limit], nil
}

func (f *fakeOutbox) MarkPublished(ctx context.Context, id int64) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.published = append(f.published, id)
	return nil
}

func (f *fakeOutbox) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	panic("not used")
}

type fakePublisher struct {
	events  []Event
	failIDs map[int64]error
}

func (f *fakePublisher) Publish(ctx context.Context, event Event) error {
	if err := f.failIDs[event.ID]; err != nil {
		return err
	}
	f.events = append(f.events, event)
	return nil
}

func TestPublishEnqueuesAndMarks(t *testing.T) {
	ob := &fakeOutbox{rows: []*outbox.Event{
		{ID: 1, Topic: "t.subscribed", EventKey: "a", Payload: "{}"},
		{ID: 2, Topic: "t.orphan", EventKey: "b", Payload: "{}"},
	}}
	pub := &fakePublisher{}
	bus := New(ob, pub)
	bus.Subscribe("t.subscribed", "c1", func(ctx context.Context, e Event) error { return nil })

	if err := bus.Publish(context.Background(), 10); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(pub.events) != 1 || pub.events[0].ID != 1 {
		t.Fatalf("expected only the subscribed event enqueued, got %+v", pub.events)
	}
	if len(ob.published) != 2 {
		t.Fatalf("expected both events marked published (orphan without enqueue), got %v", ob.published)
	}
}

func TestPublishStopsOnEnqueueFailure(t *testing.T) {
	ob := &fakeOutbox{rows: []*outbox.Event{
		{ID: 1, Topic: "t", EventKey: "a", Payload: "{}"},
		{ID: 2, Topic: "t", EventKey: "b", Payload: "{}"},
	}}
	pub := &fakePublisher{failIDs: map[int64]error{1: errors.New("broker down")}}
	bus := New(ob, pub)
	bus.Subscribe("t", "c1", func(ctx context.Context, e Event) error { return nil })

	if err := bus.Publish(context.Background(), 10); err == nil {
		t.Fatal("expected the enqueue failure to surface")
	}
	if len(ob.published) != 0 || len(pub.events) != 0 {
		t.Fatalf("first failure must stop the tick before any mark: published=%v enqueued=%v", ob.published, pub.events)
	}
}

func TestDeliverRunsSubscribersAndStopsOnFailure(t *testing.T) {
	bus := New(&fakeOutbox{}, &fakePublisher{})
	var ran []string
	bus.Subscribe("t", "c1", func(ctx context.Context, e Event) error { ran = append(ran, "c1"); return nil })
	bus.Subscribe("t", "c2", func(ctx context.Context, e Event) error { ran = append(ran, "c2"); return errors.New("boom") })
	bus.Subscribe("t", "c3", func(ctx context.Context, e Event) error { ran = append(ran, "c3"); return nil })

	err := bus.Deliver(context.Background(), Event{ID: 7, Topic: "t", Key: "k"})
	if err == nil {
		t.Fatal("expected the subscriber failure to fail the delivery")
	}
	if len(ran) != 2 || ran[0] != "c1" || ran[1] != "c2" {
		t.Fatalf("expected c1,c2 then stop, got %v", ran)
	}
}

func TestDeliverWithoutSubscribersAcks(t *testing.T) {
	bus := New(&fakeOutbox{}, &fakePublisher{})
	if err := bus.Deliver(context.Background(), Event{ID: 7, Topic: "gone", Key: "k"}); err != nil {
		t.Fatalf("orphan delivery must ack, got %v", err)
	}
}
