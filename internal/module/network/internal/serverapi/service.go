// Package serverapi is the network module's node-facing server API: the
// configuration and user lists the nodes pull, cached per server and
// negotiated with ETags, and the status, online-user and traffic reports they
// push. Only the module facade may reach it.
package serverapi

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Snapshot is the per-request view of the runtime-mutable settings the node
// API consumes.
type Snapshot struct {
	Node      config.NodeConfig
	Subscribe config.SubscribeConfig
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	TrafficUsage subscription.TrafficUsage
	Redis        *redis.Client
	// Config snapshots the runtime-mutable settings per request.
	Config func() Snapshot
	// Multiplier returns the node traffic multiplier in effect at the given
	// time; nil means no multiplier is configured.
	Multiplier func(at time.Time) float32

	// The ports list only what the node API uses of each dependency, so a
	// fake implements exactly that. Servers reads the server rows,
	// Overrides their configuration overrides and Nodes a server's nodes.
	Servers   ServerReader
	Overrides OverrideReader
	Nodes     NodeLister
	// Caches keeps a server's node-facing responses under its cache
	// generation.
	Caches ServerCache
	// Status records the status reports, Online the online users.
	Status StatusRecorder
	Online OnlineRecorder
	// Subscriptions is the subscription read port (the subscription
	// facade): the subscriptions a server's nodes serve.
	Subscriptions SubscriptionReader
	// Accounts is the identity read port: which subscription owners have
	// an enabled account.
	Accounts AccountReader
}

// ServerReader reads a server row.
type ServerReader interface {
	FindOneServer(ctx context.Context, id int64) (*node.Server, error)
}

// OverrideReader reads a server's configuration override; nil means none.
type OverrideReader interface {
	FindServerConfigOverride(ctx context.Context, serverId int64) (*node.ServerConfigOverride, error)
}

// NodeLister lists the nodes a filter selects.
type NodeLister interface {
	ListNodes(ctx context.Context, params *node.FilterNodeParams) ([]*node.Node, error)
}

// ServerCache stores a server's node-facing responses only while the cache
// generation read before building them is still current.
type ServerCache interface {
	ServerCacheGeneration(ctx context.Context, serverId int64) (int64, error)
	SetServerCache(ctx context.Context, serverId int64, key string, value any, generation int64) error
}

// StatusRecorder records a status report: the reported status, a changed
// certificate pin, and the invalidation of the server's caches that carry
// the old pin.
type StatusRecorder interface {
	UpdateStatusCache(ctx context.Context, serverId int64, status *node.Status) error
	UpdateServerProtocolsIfCurrent(ctx context.Context, id int64, current, updated string) (bool, error)
	ClearServerCache(ctx context.Context, serverId int64) error
}

// OnlineRecorder records the users a server reports online.
type OnlineRecorder interface {
	UpdateOnlineUserSubscribe(ctx context.Context, serverId int64, protocol string, subscribe node.OnlineUserSubscribe) error
	UpdateOnlineUserSubscribeGlobal(ctx context.Context, subscribe node.OnlineUserSubscribe) error
}

// SubscriptionReader is the subscription read port (the subscription
// facade): the subscriptions the nodes of a scope serve, and one by id.
type SubscriptionReader interface {
	ServableSubscriptionsByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]subscription.ServedSubscription, error)
	SubscriptionByID(ctx context.Context, id int64) (*usersub.Subscribe, error)
}

// AccountReader reports which of the accounts are enabled.
type AccountReader interface {
	FindEnabledUserIDs(ctx context.Context, ids []int64) ([]int64, error)
}

// Service is the node-facing API entry point used by the network facade.
type Service struct {
	deps           Deps
	userIndexes    servedIndexCache
	indexLoads     singleflight.Group
	userListLoads  singleflight.Group
	indexBuilds    chan struct{}
	userListBuilds chan struct{}
	deviceClaims   chan struct{}
}

// NewService returns the node API's service over deps.
func NewService(deps Deps) *Service {
	return &Service{
		deps:           deps,
		indexBuilds:    make(chan struct{}, 4),
		userListBuilds: make(chan struct{}, 4),
		deviceClaims:   make(chan struct{}, 128),
	}
}
