// Package selfsub implements the user self-service subscription management
// of the subscription module: viewing, token reset, notes, and the two-phase
// cancellation whose refund stage the billing module settles. Only the
// module facade may reach it.
package selfsub

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
)

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	UserSubs UserSubscriptions
	Plans    repository.SubscribeRepo
	// Orders, Refunds, Logs and Inbox are ports onto the billing and
	// platform domains: the order a refund pays back, the refund stage of a
	// cancellation, the system log and the cancellation markers.
	Orders  OrderReader
	Refunds RefundSettler
	Cache   CacheInvalidator
	Logs    LogReader
	Inbox   MarkerReader
	Store   Store
	// SingleModel forbids holding more than one blocking subscription;
	// runtime-mutable, read per request.
	SingleModel func() bool
}

// UserSubscriptions is the part of the subscription repository the
// self-service reads and the note edit use outside a transaction; the
// module's repository provides it.
type UserSubscriptions interface {
	FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error)
	// FindOneUserSubscribe returns the subscription with its plan.
	FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error)
	// QueryUserSubscribe lists the user's subscriptions with their plans;
	// given statuses, only those in one of them.
	QueryUserSubscribe(ctx context.Context, userID int64, statuses ...int64) ([]*usersub.SubscribeDetails, error)
	// UpdateSubscribeColumns writes only the named columns of data.Id.
	UpdateSubscribeColumns(ctx context.Context, data *usersub.Subscribe, columns ...string) error
}

// OrderReader reads an order with its renewals: what a refund pays back.
type OrderReader interface {
	FindOneDetails(ctx context.Context, id int64) (*order.Details, error)
}

// RefundSettler is the billing port of the cancellation's refund stage; the
// billing facade provides it.
type RefundSettler interface {
	// UnsubscribeRefundSettled reports whether the subscription's refund was
	// settled.
	UnsubscribeRefundSettled(ctx context.Context, subscriptionID int64) (bool, error)
	// SettleUnsubscribeRefund settles it in a billing-domain transaction:
	// amount back to the user's wallet and the referral commission taken
	// back when the subscription has an order, and the refund marker; a
	// second settlement fails.
	SettleUnsubscribeRefund(ctx context.Context, userID, subscriptionID, orderID, amount int64) error
}

// CacheInvalidator drops cached subscription rows.
type CacheInvalidator interface {
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
}

// LogReader pages the platform's system log.
type LogReader interface {
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
}

// MarkerReader reads the inbox markers of the cancellation stage.
type MarkerReader interface {
	Find(ctx context.Context, consumer, eventKey string) (*inbox.Record, error)
}

// Service is the self-service entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Store is the persistence capability required by this package: the
// cancellation's subscription-domain transaction.
type Store interface {
	repository.SubscriptionTransactor
}
