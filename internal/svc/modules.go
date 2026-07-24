package svc

import (
	"context"

	"github.com/perfect-panel/server/internal/module/support"
	"github.com/perfect-panel/server/internal/repository"
)

// newSupportModule wires the support module against the legacy store. The
// adapters below satisfy the module's ports until the owning modules exist
// (ADR-001).
func newSupportModule(store repository.Store) support.Service {
	return support.New(support.Deps{
		Announcements: store.Announcement(),
		Ads:           store.Ads(),
		Documents:     store.Document(),
		Tickets:       store.Ticket(),
		Subscriptions: subscriptionReader{store: store},
	})
}

// subscriptionReader adapts the legacy user-subscription repository to the
// support module's SubscriptionReader port.
type subscriptionReader struct {
	store repository.Store
}

func (r subscriptionReader) HasActiveSubscription(ctx context.Context, userID int64) (bool, error) {
	// status 1 = active
	subs, err := r.store.UserSubscription().QueryUserSubscribe(ctx, userID, 1)
	if err != nil {
		return false, err
	}
	return len(subs) > 0, nil
}
