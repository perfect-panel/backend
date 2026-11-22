package order

import (
	"context"

	"github.com/pars-aria-labs/asynq"
)

// orderEventPublisher is billing's order-event outbox publication.
type orderEventPublisher interface {
	PublishOrderEvents(ctx context.Context) error
}

// PublishOrderEventsHandler is the queue shell of billing's order-event
// outbox: the billing module drains the durable events onto the channels that
// wake the order event streams (ADR-001), and the scheduler's next tick
// drains whatever a run left behind.
type PublishOrderEventsHandler struct {
	outbox orderEventPublisher
}

// NewPublishOrderEventsHandler builds the shell over the billing facade.
func NewPublishOrderEventsHandler(deps Dependencies) *PublishOrderEventsHandler {
	return &PublishOrderEventsHandler{outbox: deps.Billing}
}

// ProcessTask runs one publication and hands its failure to asynq.
func (h *PublishOrderEventsHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	return h.outbox.PublishOrderEvents(ctx)
}
