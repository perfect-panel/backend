package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
)

// SubscriptionCaches is the subscription module's cache invalidation for
// accounts whose access ended (the subscription facade).
type SubscriptionCaches interface {
	// ClearUserSubscriptionCaches drops the cached subscriptions of the
	// users and returns the node scope of their plans.
	ClearUserSubscriptionCaches(ctx context.Context, userIDs []int64) (nodeIDs []int64, tags []string, err error)
}

// ServerCaches is the network module's invalidation of the node-facing
// server caches (the network facade).
type ServerCaches interface {
	// ClearServerCachesByNodeScope drops the caches of every server
	// carrying a node the scope selects.
	ClearServerCachesByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) error
}

// clearUserAccessCaches drops, best effort, the caches that keep serving the
// accounts, so a deletion or a disabling takes effect at once for
// subscription tokens and the node-facing credential lists: the subscription
// module clears their subscription entries and reports the node scope of
// their plans, and the network module then clears the server caches of that
// whole scope at once. The database rows are left alone; the account-state
// checks stay authoritative. Failures are logged; the caches expire on their
// own.
func clearUserAccessCaches(ctx context.Context, deps Deps, userIDs []int64) {
	if deps.SubscriptionCaches == nil || deps.ServerCaches == nil {
		return
	}
	log := logger.WithContext(ctx)
	userIDs = slicesx.RemoveDuplicateElements(userIDs...)
	nodeIDs, tags, err := deps.SubscriptionCaches.ClearUserSubscriptionCaches(ctx, userIDs)
	if err != nil {
		log.Errorw("clear the subscription caches of users whose access ended", logger.Field("user_ids", userIDs), logger.Field("error", err.Error()))
		return
	}
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return
	}
	if err := deps.ServerCaches.ClearServerCachesByNodeScope(ctx, nodeIDs, tags); err != nil {
		log.Errorw("clear the node caches of users whose access ended", logger.Field("user_ids", userIDs), logger.Field("error", err.Error()))
	}
}
