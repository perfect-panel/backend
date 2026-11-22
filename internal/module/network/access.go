package network

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/network/internal/adminserver"
	"github.com/perfect-panel/backend/internal/module/network/internal/edge"
	"github.com/perfect-panel/backend/internal/module/network/internal/serverapi"
)

// Access is the part of the facade the other modules compose (ADR-001 rule
// 4): entity-typed reads they assemble in memory instead of reading the
// network tables, and the invalidation of the node-facing caches.
type Access interface {
	// ListEnabledNodesByScope lists the enabled nodes, with their servers,
	// that a plan scope selects (an explicit node id or any of the tags) in
	// sort order. An empty scope selects every enabled node.
	ListEnabledNodesByScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error)
	// ListEnabledNodes lists up to limit enabled nodes, with their servers,
	// in sort order.
	ListEnabledNodes(ctx context.Context, limit int) ([]*node.Node, error)
	// SubscriptionTrafficLogs pages the traffic records of a user's
	// subscription and counts them.
	SubscriptionTrafficLogs(ctx context.Context, userID, subscribeID int64, page, size int) ([]*traffic.TrafficLog, int64, error)
	// ClearServerCachesByNodeScope drops the node-facing caches (user lists
	// and configs) of every server carrying a node the scope selects, an
	// explicit node id or any of the tags, enabled or not. An empty scope
	// clears nothing.
	ClearServerCachesByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) error
}

// SubscriptionReader is the subscription module's read surface the network
// flows compose (ADR-001 rule 4); the subscription facade satisfies it.
type SubscriptionReader interface {
	adminserver.OnlineSubscriptionReader
	serverapi.SubscriptionReader
	edge.SubscriptionReader
}

func (s *service) ListEnabledNodesByScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error) {
	enabled := true
	return s.store.Node().ListNodesByScope(ctx, nodeIDs, tags, &enabled, true)
}

func (s *service) ListEnabledNodes(ctx context.Context, limit int) ([]*node.Node, error) {
	enabled := true
	_, nodes, err := s.store.Node().FilterNodeList(ctx, &node.FilterNodeParams{
		Page:    1,
		Size:    limit,
		Preload: true,
		Enabled: &enabled,
	})
	return nodes, err
}

func (s *service) SubscriptionTrafficLogs(ctx context.Context, userID, subscribeID int64, page, size int) ([]*traffic.TrafficLog, int64, error) {
	return s.store.TrafficLog().QueryTrafficLogPageList(ctx, userID, subscribeID, page, size)
}

func (s *service) ClearServerCachesByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) error {
	return s.admin.ClearServerCachesByNodeScope(ctx, nodeIDs, tags)
}
