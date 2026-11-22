package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/eventbus"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
)

// DeliverDomainEventHandler is the delivery worker: each task carries one
// outbox event, and the bus runs every subscriber of its topic. A failing
// subscriber fails the task so asynq retries with backoff and eventually
// archives it (the dead-letter queue); subscribers are idempotent, so
// retries and duplicate deliveries are safe.
type DeliverDomainEventHandler struct {
	bus *eventbus.Bus
}

// NewDeliverDomainEventHandler builds the delivery worker over the bus.
func NewDeliverDomainEventHandler(bus *eventbus.Bus) *DeliverDomainEventHandler {
	return &DeliverDomainEventHandler{bus: bus}
}

func (h *DeliverDomainEventHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload taskqueue.EventDeliverPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		// A malformed payload can never deliver; retrying cannot fix it.
		return fmt.Errorf("unmarshal event payload: %w: %w", err, asynq.SkipRetry)
	}
	return h.bus.Deliver(ctx, eventbus.Event{
		ID:      payload.ID,
		Topic:   payload.Topic,
		Key:     payload.Key,
		Payload: payload.Payload,
	})
}
