// Package events hosts the queue shells of the domain-event bus: the publish
// pump moving outbox rows onto the asynq broker, and the delivery worker
// running subscribers.
package events

import (
	"context"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/eventbus"
)

// dispatchBatchSize bounds the events one tick moves onto the queue; the
// next tick, five seconds later, picks up the rest.
const dispatchBatchSize = 500

// DispatchDomainEventsHandler is the publish pump: it drains the generic
// domain-event outbox onto the asynq queue. Enqueues deduplicate by outbox
// event id, so the tick can retry freely.
type DispatchDomainEventsHandler struct {
	bus *eventbus.Bus
}

// NewDispatchDomainEventsHandler builds the publish pump over the bus.
func NewDispatchDomainEventsHandler(bus *eventbus.Bus) *DispatchDomainEventsHandler {
	return &DispatchDomainEventsHandler{bus: bus}
}

func (h *DispatchDomainEventsHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	return h.bus.Publish(ctx, dispatchBatchSize)
}
