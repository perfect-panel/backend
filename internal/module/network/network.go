// Package network is the facade of the network module: admin-side server and
// node management, the node-facing server API and the edge client manifest.
// See docs/design/adr-001-modular-monolith.md.
package network

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/config"
	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/internal/adminserver"
	"github.com/perfect-panel/server/internal/module/network/internal/edge"
	"github.com/perfect-panel/server/internal/module/network/internal/repo"
	"github.com/perfect-panel/server/internal/module/network/internal/serverapi"
	"github.com/perfect-panel/server/internal/module/network/internal/trafficstat"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// Admin-side server and node management.
	CreateServer(ctx context.Context, req *dto.CreateServerRequest) error
	UpdateServer(ctx context.Context, req *dto.UpdateServerRequest) error
	DeleteServer(ctx context.Context, req *dto.DeleteServerRequest) error
	FilterServerList(ctx context.Context, req *dto.FilterServerListRequest) (*dto.FilterServerListResponse, error)
	ResetSortWithServer(ctx context.Context, req *dto.ResetSortRequest) error
	GetServerProtocols(ctx context.Context, req *dto.GetServerProtocolsRequest) (*dto.GetServerProtocolsResponse, error)
	GetServerNodeConfig(ctx context.Context, req *dto.GetServerNodeConfigRequest) (*dto.GetServerNodeConfigResponse, error)
	UpdateServerNodeConfig(ctx context.Context, req *dto.UpdateServerNodeConfigRequest) error
	CreateNode(ctx context.Context, req *dto.CreateNodeRequest) error
	UpdateNode(ctx context.Context, req *dto.UpdateNodeRequest) error
	DeleteNode(ctx context.Context, req *dto.DeleteNodeRequest) error
	FilterNodeList(ctx context.Context, req *dto.FilterNodeListRequest) (*dto.FilterNodeListResponse, error)
	ToggleNodeStatus(ctx context.Context, req *dto.ToggleNodeStatusRequest) error
	ResetSortWithNode(ctx context.Context, req *dto.ResetSortRequest) error
	QueryNodeTag(ctx context.Context) (*dto.QueryNodeTagResponse, error)

	// Statistics serves the platform's reads of the node and traffic
	// figures.
	Statistics

	// The node-facing server API. The ETag negotiation flows through
	// RequestMeta/ResponseMeta; node authentication stays in the handlers.
	GetServerConfig(ctx context.Context, req *dto.GetServerConfigRequest, meta RequestMeta) (*dto.GetServerConfigResponse, ResponseMeta, error)
	GetServerUserList(ctx context.Context, req *dto.GetServerUserListRequest, meta RequestMeta) (*dto.GetServerUserListResponse, ResponseMeta, error)
	QueryServerProtocolConfig(ctx context.Context, req *dto.QueryServerConfigRequest) (*dto.QueryServerConfigResponse, error)
	PushOnlineUsers(ctx context.Context, req *dto.OnlineUsersRequest) error
	GetServerAliveList(ctx context.Context, req *dto.GetServerUserListRequest) (*dto.GetServerAliveListResponse, error)
	ClaimServerDevice(ctx context.Context, req *dto.ClaimServerDeviceRequest) (*dto.ClaimServerDeviceResponse, error)
	ServerPushStatus(ctx context.Context, req *dto.ServerPushStatusRequest) error
	ServerPushUserTraffic(ctx context.Context, req *dto.ServerPushUserTrafficRequest) error

	// EdgeManifest serves the token-authenticated edge client manifest.
	EdgeManifest(ctx context.Context, token string) (*dto.EdgeManifestResponse, error)

	// RecordDailyTrafficStatistics records the previous day's traffic
	// statistics in the system log, once per day however often it runs.
	RecordDailyTrafficStatistics(ctx context.Context) error
	// PruneTrafficLogs deletes the raw traffic log older than before and
	// returns how many rows it deleted.
	PruneTrafficLogs(ctx context.Context, before time.Time) (int64, error)

	// Access serves the other modules' reads and cache invalidation (see
	// access.go).
	Access
}

// ErrManifestNotFound re-exports the edge subdomain's not-found sentinel for
// the transport handler's status mapping.
var ErrManifestNotFound = edge.ErrManifestNotFound

// RequestMeta and ResponseMeta re-export the server API's conditional-request
// negotiation types for the transport handlers.
type (
	RequestMeta  = serverapi.RequestMeta
	ResponseMeta = serverapi.ResponseMeta
)

// Snapshot is the per-request view of the runtime-mutable settings the
// network flows consume.
type Snapshot struct {
	Node      config.NodeConfig
	Subscribe config.SubscribeConfig
}

// Deps declares everything the module needs; the composition root
// (internal/app) provides them.
type Deps struct {
	Store        Store
	TrafficUsage subscription.TrafficUsage
	// Subscription is the subscription read port (the subscription facade).
	Subscription SubscriptionReader
	Redis        *redis.Client
	// Config snapshots the runtime-mutable settings per request.
	Config func() Snapshot
	// Multiplier returns the node traffic multiplier in effect at the given
	// time; nil means no multiplier is configured.
	Multiplier func(at time.Time) float32
	// Accounts reads the subscription owners' account state from the
	// identity domain.
	Accounts Accounts
	// Logs is the platform's system log, where the daily traffic statistics
	// are recorded.
	Logs repository.LogRepo
}

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly. The node cache retrier is a background singleton that
// survives per-transaction rebuilds, so it lives in this closure
// (ADR-001 step-6 preparation).
func NewRepoBuilder(rds *redis.Client) repository.NetworkBuilder {
	retrier := repo.NewServerCacheInvalidationRetrier(rds)
	return func(c repository.ModuleConn) repository.NetworkRepos {
		nodes := repo.NewNodeRepo(c.DB, c.Redis, retrier)
		return repository.NetworkRepos{
			Nodes:    nodes,
			Traffic:  repo.NewTrafficRepo(c.DB),
			NodeKeys: nodes.(repository.NodeCacheKeyBridge),
		}
	}
}

// New assembles the module's service from deps.
func New(deps Deps) Service {
	nodes := deps.Store.Node()
	return &service{
		statistics: statistics{store: deps.Store},
		admin: adminserver.NewService(adminserver.Deps{
			Store: deps.Store,
			Config: func() adminserver.Snapshot {
				return adminserver.Snapshot{Node: deps.Config().Node}
			},
			Subscriptions: deps.Subscription,
		}),
		api: serverapi.NewService(serverapi.Deps{
			TrafficUsage: deps.TrafficUsage,
			Redis:        deps.Redis,
			Config: func() serverapi.Snapshot {
				cfg := deps.Config()
				return serverapi.Snapshot{Node: cfg.Node, Subscribe: cfg.Subscribe}
			},
			Multiplier: deps.Multiplier,
			// The node API's ports: the network's own node data, the
			// subscription reads and the identity account check.
			Servers:       nodes,
			Overrides:     nodes,
			Nodes:         nodes,
			Caches:        nodes,
			Status:        nodes,
			Online:        nodes,
			Subscriptions: deps.Subscription,
			Accounts:      deps.Accounts,
		}),
		edge: edge.NewService(edge.DepsFrom(deps.Store, deps.Subscription, deps.Accounts, func() edge.Snapshot {
			return edge.Snapshot{Subscribe: deps.Config().Subscribe}
		})),
		stats: trafficstat.NewService(trafficstat.Deps{
			Traffic: deps.Store.TrafficLog(),
			Logs:    deps.Logs,
		}),
		store: deps.Store,
	}
}

type service struct {
	statistics
	admin *adminserver.Service
	api   *serverapi.Service
	edge  *edge.Service
	stats *trafficstat.Service
	store Store
}

func (s *service) CreateServer(ctx context.Context, req *dto.CreateServerRequest) error {
	return s.admin.CreateServer(ctx, req)
}

func (s *service) UpdateServer(ctx context.Context, req *dto.UpdateServerRequest) error {
	return s.admin.UpdateServer(ctx, req)
}

func (s *service) DeleteServer(ctx context.Context, req *dto.DeleteServerRequest) error {
	return s.admin.DeleteServer(ctx, req)
}

func (s *service) FilterServerList(ctx context.Context, req *dto.FilterServerListRequest) (*dto.FilterServerListResponse, error) {
	return s.admin.FilterServerList(ctx, req)
}

func (s *service) ResetSortWithServer(ctx context.Context, req *dto.ResetSortRequest) error {
	return s.admin.ResetSortWithServer(ctx, req)
}

func (s *service) GetServerProtocols(ctx context.Context, req *dto.GetServerProtocolsRequest) (*dto.GetServerProtocolsResponse, error) {
	return s.admin.GetServerProtocols(ctx, req)
}

func (s *service) GetServerNodeConfig(ctx context.Context, req *dto.GetServerNodeConfigRequest) (*dto.GetServerNodeConfigResponse, error) {
	return s.admin.GetServerNodeConfig(ctx, req)
}

func (s *service) UpdateServerNodeConfig(ctx context.Context, req *dto.UpdateServerNodeConfigRequest) error {
	return s.admin.UpdateServerNodeConfig(ctx, req)
}

func (s *service) CreateNode(ctx context.Context, req *dto.CreateNodeRequest) error {
	return s.admin.CreateNode(ctx, req)
}

func (s *service) UpdateNode(ctx context.Context, req *dto.UpdateNodeRequest) error {
	return s.admin.UpdateNode(ctx, req)
}

func (s *service) DeleteNode(ctx context.Context, req *dto.DeleteNodeRequest) error {
	return s.admin.DeleteNode(ctx, req)
}

func (s *service) FilterNodeList(ctx context.Context, req *dto.FilterNodeListRequest) (*dto.FilterNodeListResponse, error) {
	return s.admin.FilterNodeList(ctx, req)
}

func (s *service) ToggleNodeStatus(ctx context.Context, req *dto.ToggleNodeStatusRequest) error {
	return s.admin.ToggleNodeStatus(ctx, req)
}

func (s *service) ResetSortWithNode(ctx context.Context, req *dto.ResetSortRequest) error {
	return s.admin.ResetSortWithNode(ctx, req)
}

func (s *service) QueryNodeTag(ctx context.Context) (*dto.QueryNodeTagResponse, error) {
	return s.admin.QueryNodeTag(ctx)
}

func (s *service) GetServerConfig(ctx context.Context, req *dto.GetServerConfigRequest, meta RequestMeta) (*dto.GetServerConfigResponse, ResponseMeta, error) {
	return s.api.GetServerConfig(ctx, req, meta)
}

func (s *service) GetServerUserList(ctx context.Context, req *dto.GetServerUserListRequest, meta RequestMeta) (*dto.GetServerUserListResponse, ResponseMeta, error) {
	return s.api.GetServerUserList(ctx, req, meta)
}

func (s *service) QueryServerProtocolConfig(ctx context.Context, req *dto.QueryServerConfigRequest) (*dto.QueryServerConfigResponse, error) {
	return s.api.QueryServerProtocolConfig(ctx, req)
}

func (s *service) PushOnlineUsers(ctx context.Context, req *dto.OnlineUsersRequest) error {
	return s.api.PushOnlineUsers(ctx, req)
}

func (s *service) GetServerAliveList(ctx context.Context, req *dto.GetServerUserListRequest) (*dto.GetServerAliveListResponse, error) {
	return s.api.GetServerAliveList(ctx, req)
}

func (s *service) ClaimServerDevice(ctx context.Context, req *dto.ClaimServerDeviceRequest) (*dto.ClaimServerDeviceResponse, error) {
	return s.api.ClaimServerDevice(ctx, req)
}

func (s *service) ServerPushStatus(ctx context.Context, req *dto.ServerPushStatusRequest) error {
	return s.api.ServerPushStatus(ctx, req)
}

func (s *service) ServerPushUserTraffic(ctx context.Context, req *dto.ServerPushUserTrafficRequest) error {
	return s.api.ServerPushUserTraffic(ctx, req)
}

func (s *service) EdgeManifest(ctx context.Context, token string) (*dto.EdgeManifestResponse, error) {
	return s.edge.Manifest(ctx, token)
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	adminserver.Store
	edge.Store
	// TrafficLog serves the traffic-log reads of the other modules.
	TrafficLog() repository.TrafficRepo
}

func (s *service) RecordDailyTrafficStatistics(ctx context.Context) error {
	return s.stats.RecordDailyStatistics(ctx)
}

func (s *service) PruneTrafficLogs(ctx context.Context, before time.Time) (int64, error) {
	return s.stats.PruneTrafficLogs(ctx, before)
}
