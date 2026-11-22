package order

import (
	"context"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	"github.com/perfect-panel/server/internal/module/billing"
	orderEntity "github.com/perfect-panel/server/internal/module/billing/entity/order"
	"github.com/perfect-panel/server/internal/module/notification"
)

// Dependencies are what the order task handlers are built from. Billing's
// tables are reached only through the billing facade; each handler narrows
// the facade to the methods it calls.
type Dependencies struct {
	Queue        *taskqueue.Client
	Inspector    *asynq.Inspector
	Billing      billing.Service
	Notification notification.Service
	Telegram     func() config.Telegram
	// Outbox and Inbox are the platform kernel's domain-event outbox and
	// idempotent inbox, which the order-event cleanup prunes under the same
	// replay contract as billing's order events.
	Outbox PublishedEventPruner
	Inbox  ProcessedMarkerPruner
}

func (deps Dependencies) telegramConfig() config.Telegram {
	if deps.Telegram == nil {
		return config.Telegram{}
	}
	return deps.Telegram()
}

// PublishedEventPruner deletes the domain events the event bus published
// before cutoff; the platform kernel's outbox provides it.
type PublishedEventPruner interface {
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// ProcessedMarkerPruner deletes the inbox markers of the steps processed
// before cutoff; the platform kernel's inbox provides it.
type ProcessedMarkerPruner interface {
	DeleteProcessedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// orderScanner pages billing's orders of one status by ascending id; the
// billing facade provides it.
type orderScanner interface {
	OrdersByStatusAfter(ctx context.Context, status uint8, afterID int64, limit int) ([]*orderEntity.Order, error)
}
