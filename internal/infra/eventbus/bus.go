// Package eventbus is the domain-event bus riding the asynq message queue
// (ADR-001 step-6 preparation). Producers append events to the outbox inside
// their domain transaction; the publish pump hands unpublished events to the
// broker and marks them published on successful enqueue; the queue worker
// delivers each event to every subscriber of its topic, with the broker
// owning retries, backoff and the dead-letter archive. Delivery is
// at-least-once and subscribers are expected to be idempotent (the inbox
// pattern). Replacing asynq with another broker changes only the Publisher
// adapter and the worker shell.
package eventbus

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Event is the payload handed to subscribers.
type Event struct {
	ID      int64
	Topic   string
	Key     string
	Payload string
	// TraceCarrier is the producing request's serialized trace context
	// (from the outbox row); the broker adapter resumes it so the delivery
	// joins the originating trace.
	TraceCarrier string
}

// Handler processes one event. Returning an error fails the delivery task so
// the broker retries it; handlers must therefore be idempotent.
type Handler func(ctx context.Context, event Event) error

// Publisher enqueues an event on the message broker. Publishing the same
// event twice must be safe: the pump replays an enqueue when marking the
// event published fails, so the adapter deduplicates by event ID where the
// broker allows and subscribers stay idempotent regardless.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

type subscription struct {
	consumer string
	handler  Handler
}

// Bus routes outbox events to subscribers by topic.
type Bus struct {
	outbox      repository.OutboxRepo
	publisher   Publisher
	subscribers map[string][]subscription
}

// New returns a bus that reads the events to publish from outboxRepo and
// hands them to publisher; subscribers are registered with Subscribe.
func New(outboxRepo repository.OutboxRepo, publisher Publisher) *Bus {
	return &Bus{
		outbox:      outboxRepo,
		publisher:   publisher,
		subscribers: make(map[string][]subscription),
	}
}

// Subscribe registers a consumer for a topic. The consumer name is the
// subscriber's identity for logging and mirrors the inbox consumer it uses
// for idempotency. Registration happens at composition time, before
// publishing and delivery start; Subscribe is not safe for concurrent use
// with them.
func (b *Bus) Subscribe(topic, consumer string, handler Handler) {
	b.subscribers[topic] = append(b.subscribers[topic], subscription{consumer: consumer, handler: handler})
}

// Publish drains up to limit unpublished outbox events onto the broker,
// marking each published once its enqueue succeeded. A failing enqueue stops
// the tick so events enter the broker in outbox order; ordering between
// deliveries is then the broker's (concurrent workers), which subscribers
// already tolerate via their per-key serialization and inbox markers.
// Events whose topic has no subscribers are marked published without an
// enqueue so an orphaned topic cannot wedge the pump.
func (b *Bus) Publish(ctx context.Context, limit int) error {
	events, err := b.outbox.ListUnpublished(ctx, limit)
	if err != nil {
		return err
	}
	for _, row := range events {
		if len(b.subscribers[row.Topic]) > 0 {
			event := Event{ID: row.ID, Topic: row.Topic, Key: row.EventKey, Payload: row.Payload, TraceCarrier: row.TraceCarrier}
			if err := b.publisher.Publish(ctx, event); err != nil {
				return fmt.Errorf("publish event %d on %s: %w", row.ID, row.Topic, err)
			}
		}
		if err := b.outbox.MarkPublished(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// Deliver runs every subscriber of the event's topic; the queue worker calls
// it once per delivery task. The first failing subscriber fails the task so
// the broker retries the whole event — subscribers that already succeeded
// re-run and skip via their inbox markers.
func (b *Bus) Deliver(ctx context.Context, event Event) error {
	subs := b.subscribers[event.Topic]
	if len(subs) == 0 {
		// A topic can lose its last subscriber between enqueue and delivery
		// (e.g. across a deploy); ack rather than retry forever.
		logger.WithContext(ctx).Errorw("[EventBus] no subscribers for delivered event; dropping",
			logger.Field("topic", event.Topic), logger.Field("key", event.Key))
		return nil
	}
	for _, sub := range subs {
		if err := sub.handler(ctx, event); err != nil {
			logger.WithContext(ctx).Errorw("[EventBus] subscriber failed; broker will retry",
				logger.Field("topic", event.Topic), logger.Field("key", event.Key),
				logger.Field("consumer", sub.consumer), logger.Field("error", err.Error()))
			return fmt.Errorf("subscriber %s on %s: %w", sub.consumer, event.Topic, err)
		}
	}
	return nil
}
