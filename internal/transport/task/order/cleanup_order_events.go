package order

import (
	"context"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// orderEventRetention is the replay contract billing's order events and the
// platform kernel's delivery records share.
const orderEventRetention = 30 * 24 * time.Hour

// orderEventCleaner is billing's order-event retention cleanup.
type orderEventCleaner interface {
	CleanupOrderEvents(ctx context.Context, cutoff time.Time) (int64, error)
}

// CleanupOrderEventsHandler applies the replay contract to billing's order
// events and to the platform kernel's published domain events and inbox
// markers. Billing removes only the order events that have already reached
// Redis and are older than the contract: unpublished events are never
// deleted, even if an outage lasts longer than the normal retention period.
type CleanupOrderEventsHandler struct {
	orderEvents orderEventCleaner
	outbox      PublishedEventPruner
	inbox       ProcessedMarkerPruner
}

// NewCleanupOrderEventsHandler builds the cleanup over the billing facade and
// the platform kernel's outbox and inbox.
func NewCleanupOrderEventsHandler(deps Dependencies) *CleanupOrderEventsHandler {
	return &CleanupOrderEventsHandler{orderEvents: deps.Billing, outbox: deps.Outbox, inbox: deps.Inbox}
}

// ProcessTask runs the cleanups in turn under one cutoff and returns the
// first failure for asynq to retry the task; repeating the cleanups that
// already ran is harmless.
func (h *CleanupOrderEventsHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	cutoff := timeutil.Now().Add(-orderEventRetention)
	deleted, err := h.orderEvents.CleanupOrderEvents(ctx, cutoff)
	if err != nil {
		return err
	}
	if deleted > 0 {
		logger.WithContext(ctx).Infof("removed %d expired order events", deleted)
	}
	// The idempotent inbox shares the retention contract: every consumer's
	// replay window (deferred closes, activation retries, bucket flushes)
	// resolves far inside it.
	outboxDeleted, err := h.outbox.DeletePublishedBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	if outboxDeleted > 0 {
		logger.WithContext(ctx).Infof("cleaned up %d published domain events", outboxDeleted)
	}
	inboxDeleted, err := h.inbox.DeleteProcessedBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	if inboxDeleted > 0 {
		logger.WithContext(ctx).Infof("removed %d expired inbox markers", inboxDeleted)
	}
	return nil
}
