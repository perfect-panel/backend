package subscription

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	usersubEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/application"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/delivery"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/nodeaccess"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/storefront"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/usersub"
)

// Access is the part of the facade the other modules compose (ADR-001 rule
// 4): entity-typed reads they assemble in memory instead of reading the
// subscription tables, and the cache invalidation when an account's access
// ends. Nothing here opens a transaction.
type Access interface {
	// SubscriptionByToken returns the subscription holding the token. An
	// unknown token reports gorm.ErrRecordNotFound.
	SubscriptionByToken(ctx context.Context, token string) (*usersubEntity.Subscribe, error)
	// SubscriptionByID returns the subscription. An unknown id reports
	// gorm.ErrRecordNotFound.
	SubscriptionByID(ctx context.Context, id int64) (*usersubEntity.Subscribe, error)
	// SubscriptionsByIDs returns the subscriptions among the ids that exist.
	SubscriptionsByIDs(ctx context.Context, ids []int64) ([]*usersubEntity.Subscribe, error)
	// SubscriptionDetailsByIDs returns the subscriptions among the ids that
	// exist, each with its plan.
	SubscriptionDetailsByIDs(ctx context.Context, ids []int64) ([]*usersubEntity.SubscribeDetails, error)
	// PlanByID returns the plan.
	PlanByID(ctx context.Context, id int64) (*subscribe.Subscribe, error)
	// ServableSubscriptionsByNodeScope returns the subscriptions the nodes
	// of a scope may serve now: the servable subscriptions of the plans
	// selecting any of the node ids or tags, by plan and id, each with its
	// plan.
	ServableSubscriptionsByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]ServedSubscription, error)
	// ClearUserSubscriptionCaches drops the cached entries of every
	// subscription the users hold, so the tokens of a deleted or disabled
	// account stop resolving from the cache, and returns the node scope of
	// their plans: the node user lists that still carry the subscriptions.
	// A failed cache deletion is logged; the scope is returned all the same.
	ClearUserSubscriptionCaches(ctx context.Context, userIDs []int64) (nodeIDs []int64, tags []string, err error)
	// ClientApplications lists the client applications in their stored
	// order.
	ClientApplications(ctx context.Context) ([]*client.SubscribeApplication, error)
}

// ServedSubscription re-exports the node-access subdomain's served
// subscription: a subscription a node may serve now, with the plan whose
// speed and device limits apply to it.
type ServedSubscription = nodeaccess.Served

// NodeReader and TrafficLogReader are the network read ports (the network
// facade satisfies them): the nodes delivery, the storefront and the
// template preview list, and the admin view of a subscription's traffic.
type (
	NodeReader interface {
		delivery.NodeLister
		storefront.NodeLister
		application.NodeLister
	}
	TrafficLogReader = usersub.TrafficLogReader
)

func (s *service) SubscriptionByToken(ctx context.Context, token string) (*usersubEntity.Subscribe, error) {
	return s.nodeAccess.SubscriptionByToken(ctx, token)
}

func (s *service) SubscriptionByID(ctx context.Context, id int64) (*usersubEntity.Subscribe, error) {
	return s.nodeAccess.SubscriptionByID(ctx, id)
}

func (s *service) SubscriptionsByIDs(ctx context.Context, ids []int64) ([]*usersubEntity.Subscribe, error) {
	return s.nodeAccess.SubscriptionsByIDs(ctx, ids)
}

func (s *service) SubscriptionDetailsByIDs(ctx context.Context, ids []int64) ([]*usersubEntity.SubscribeDetails, error) {
	return s.nodeAccess.SubscriptionDetailsByIDs(ctx, ids)
}

func (s *service) PlanByID(ctx context.Context, id int64) (*subscribe.Subscribe, error) {
	return s.nodeAccess.PlanByID(ctx, id)
}

func (s *service) ServableSubscriptionsByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]ServedSubscription, error) {
	return s.nodeAccess.ServableByNodeScope(ctx, nodeIDs, tags)
}

func (s *service) ClearUserSubscriptionCaches(ctx context.Context, userIDs []int64) ([]int64, []string, error) {
	return s.nodeAccess.ClearUserCaches(ctx, userIDs)
}

func (s *service) ClientApplications(ctx context.Context) ([]*client.SubscribeApplication, error) {
	return s.apps.ClientApplications(ctx)
}
