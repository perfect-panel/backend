package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
)

// NodeRepo manages the servers, their nodes and per-server configuration
// overrides, together with the cached node status and online users.
type NodeRepo interface {
	// server
	InsertServer(ctx context.Context, data *node.Server) error
	FindOneServer(ctx context.Context, id int64) (*node.Server, error)
	UpdateServer(ctx context.Context, data *node.Server) error
	UpdateServerProtocolsIfCurrent(ctx context.Context, id int64, current, updated string) (bool, error)
	BatchUpdateServerLastReportedAt(ctx context.Context, reports map[int64]time.Time) error
	DeleteServer(ctx context.Context, id int64) error
	FindServerConfigOverride(ctx context.Context, serverId int64) (*node.ServerConfigOverride, error)
	SaveServerConfigOverride(ctx context.Context, data *node.ServerConfigOverride) error
	DeleteServerConfigOverride(ctx context.Context, serverId int64) error
	QueryServerList(ctx context.Context, ids []int64) (servers []*node.Server, err error)
	// node
	InsertNode(ctx context.Context, data *node.Node) error
	FindOneNode(ctx context.Context, id int64) (*node.Node, error)
	UpdateNode(ctx context.Context, data *node.Node) error
	DeleteNode(ctx context.Context, id int64) error
	// cache
	StatusCache(ctx context.Context, serverId int64) (node.Status, error)
	UpdateStatusCache(ctx context.Context, serverId int64, status *node.Status) error
	OnlineUserSubscribe(ctx context.Context, serverId int64, protocol string) (node.OnlineUserSubscribe, error)
	UpdateOnlineUserSubscribe(ctx context.Context, serverId int64, protocol string, subscribe node.OnlineUserSubscribe) error
	OnlineUserSubscribeGlobal(ctx context.Context) (int64, error)
	UpdateOnlineUserSubscribeGlobal(ctx context.Context, subscribe node.OnlineUserSubscribe) error
	// query
	FilterServerList(ctx context.Context, params *node.FilterParams) (int64, []*node.Server, error)
	FilterNodeList(ctx context.Context, params *node.FilterNodeParams) (int64, []*node.Node, error)
	ListNodes(ctx context.Context, params *node.FilterNodeParams) ([]*node.Node, error)
	ListNodesByScope(ctx context.Context, nodeIDs []int64, tags []string, enabled *bool, preload bool) ([]*node.Node, error)
	QueryNodeSorts(ctx context.Context) ([]node.SortItem, error)
	QueryServerSorts(ctx context.Context) ([]node.SortItem, error)
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	QueryNodeTags(ctx context.Context) ([]string, error)
	CountEnabledNodes(ctx context.Context) (int64, error)
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error)
	QueryServerAddresses(ctx context.Context) ([]string, error)
	QueryEnabledNodeProtocols(ctx context.Context) ([]string, error)
	ClearServerCache(ctx context.Context, serverId int64) error
	ServerCacheGeneration(ctx context.Context, serverId int64) (int64, error)
	SetServerCache(ctx context.Context, serverId int64, key string, value any, generation int64) error
}

// TrafficRepo manages the traffic_log table: the traffic users consumed on
// each server, and the summaries and rankings computed from it.
type TrafficRepo interface {
	InsertBatch(ctx context.Context, data []*traffic.TrafficLog, batchSize int) error
	QueryTrafficSummary(ctx context.Context, start, end time.Time) (*traffic.TotalTraffic, error)
	TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error)
	TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error)
	QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.ServerTrafficRanking, error)
	QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.UserTrafficRanking, error)
	QueryTrafficLogPageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*traffic.TrafficLog, int64, error)
	QueryTrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error)
	DeleteBeforeBatch(ctx context.Context, end time.Time, limit int) (int64, error)
}
