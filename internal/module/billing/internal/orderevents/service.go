// Package orderevents runs billing's order-event outbox. An order state
// change commits its event row in the transaction that changes the order;
// this subdomain later broadcasts each committed event on its order's Redis
// channel, which wakes the order's event streams, and deletes the published
// events the replay contract no longer needs. Publishing is separate from
// writing the event on purpose: a Redis outage may delay a stream's wake-up
// but can never roll back a committed payment state. Only the module facade
// may reach it.
package orderevents

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

// publishBatch is how many events one publication run drains. The scheduler
// starts a run every few seconds, so a larger backlog drains over several.
const publishBatch = 500

// Outbox is the durable order-event table as the publisher and the retention
// cleanup use it.
type Outbox interface {
	ListUnpublished(ctx context.Context, limit int) ([]*order.Event, error)
	MarkPublished(ctx context.Context, id int64, publishedAt time.Time) (bool, error)
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Broadcaster announces a committed event to the streams of its order. It
// only wakes them: a stream reads the events themselves from the table.
type Broadcaster interface {
	Broadcast(ctx context.Context, event *order.Event) error
}

// Service publishes and prunes the order-event outbox.
type Service struct {
	outbox      Outbox
	broadcaster Broadcaster
}

// NewService runs the outbox over its table and the broadcaster the order
// event streams listen to.
func NewService(outbox Outbox, broadcaster Broadcaster) *Service {
	return &Service{outbox: outbox, broadcaster: broadcaster}
}

// Publish broadcasts the oldest unpublished events, at most publishBatch, in
// id order and marks each published once its broadcast succeeded. The first
// failure ends the run and is returned: the failed event and those after it
// stay unpublished for the next run. A crash between a broadcast and its mark
// broadcasts that event again, which the streams tolerate because they
// deliver events by id.
func (s *Service) Publish(ctx context.Context) error {
	events, err := s.outbox.ListUnpublished(ctx, publishBatch)
	if err != nil {
		return fmt.Errorf("list the unpublished order events: %w", err)
	}
	for _, event := range events {
		if err := s.broadcaster.Broadcast(ctx, event); err != nil {
			return fmt.Errorf("broadcast order event %d of order %s: %w", event.ID, event.OrderNo, err)
		}
		if _, err := s.outbox.MarkPublished(ctx, event.ID, timeutil.Now()); err != nil {
			return fmt.Errorf("mark order event %d published: %w", event.ID, err)
		}
	}
	if len(events) > 0 {
		logger.WithContext(ctx).Debugf("published %d order events", len(events))
	}
	return nil
}

// Cleanup deletes the published events created before cutoff and reports how
// many it deleted. Unpublished events are kept whatever their age: an outage
// that outlasts the retention must not lose an event whose streams were never
// woken for it.
func (s *Service) Cleanup(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := s.outbox.DeletePublishedBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete the published order events created before %s: %w", cutoff.Format(time.DateTime), err)
	}
	return deleted, nil
}

// RedisBroadcaster publishes an event's id on its order's Redis channel, the
// channel the event streams' broker subscribes to.
type RedisBroadcaster struct {
	Client *redis.Client
}

// Broadcast fails while Redis is not configured, so the event stays
// unpublished instead of being marked without having reached a stream.
func (b RedisBroadcaster) Broadcast(ctx context.Context, event *order.Event) error {
	if b.Client == nil {
		return errors.New("redis is not configured")
	}
	return b.Client.Publish(ctx, order.EventChannel(event.OrderNo), strconv.FormatInt(event.ID, 10)).Err()
}
