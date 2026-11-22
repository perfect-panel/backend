package network

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/repository"
)

// Statistics is the part of the facade the platform's dashboards, audit
// views and public statistics read network's figures through, instead of
// network's repositories (ADR-001 rule 4). The reads return the
// repositories' results and errors as they are.
type Statistics interface {
	// CountEnabledNodes counts the enabled nodes.
	CountEnabledNodes(ctx context.Context) (int64, error)
	// ListServerAddresses returns the address of every server.
	ListServerAddresses(ctx context.Context) ([]string, error)
	// ListEnabledNodeProtocols returns the protocol of every enabled node.
	ListEnabledNodeProtocols(ctx context.Context) ([]string, error)
	// CountServersByReportStatus counts the servers that reported after
	// cutoff (online) and the others (offline).
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (online, offline int64, err error)
	// CountOnlineUsers counts the subscriptions online on any server.
	CountOnlineUsers(ctx context.Context) (int64, error)
	// FindServers returns the servers among ids that exist.
	FindServers(ctx context.Context, ids []int64) ([]*node.Server, error)

	// TrafficSummary sums the traffic logged in [start, end).
	TrafficSummary(ctx context.Context, start, end time.Time) (*traffic.TotalTraffic, error)
	// TopServersTrafficByDay and TopUsersTrafficByDay rank the servers and
	// the subscriptions by the traffic of date's day, at most limit of them.
	TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error)
	TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error)
	// ServerTrafficRanking and UserTrafficRanking rank every server and every
	// subscription by its traffic in [start, end).
	ServerTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.ServerTrafficRanking, error)
	UserTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.UserTrafficRanking, error)
	// TrafficLogDetails pages the traffic log entries filter selects.
	TrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error)
}

// statisticsStore is the persistence the Statistics reads use.
type statisticsStore interface {
	Node() repository.NodeRepo
	TrafficLog() repository.TrafficRepo
}

// statistics serves Statistics from the module's node and traffic
// repositories.
type statistics struct {
	store statisticsStore
}

func (s statistics) CountEnabledNodes(ctx context.Context) (int64, error) {
	return s.store.Node().CountEnabledNodes(ctx)
}

func (s statistics) ListServerAddresses(ctx context.Context) ([]string, error) {
	return s.store.Node().QueryServerAddresses(ctx)
}

func (s statistics) ListEnabledNodeProtocols(ctx context.Context) ([]string, error) {
	return s.store.Node().QueryEnabledNodeProtocols(ctx)
}

func (s statistics) CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error) {
	return s.store.Node().CountServersByReportStatus(ctx, cutoff)
}

func (s statistics) CountOnlineUsers(ctx context.Context) (int64, error) {
	return s.store.Node().OnlineUserSubscribeGlobal(ctx)
}

func (s statistics) FindServers(ctx context.Context, ids []int64) ([]*node.Server, error) {
	return s.store.Node().QueryServerList(ctx, ids)
}

func (s statistics) TrafficSummary(ctx context.Context, start, end time.Time) (*traffic.TotalTraffic, error) {
	return s.store.TrafficLog().QueryTrafficSummary(ctx, start, end)
}

func (s statistics) TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error) {
	return s.store.TrafficLog().TopServersTrafficByDay(ctx, date, limit)
}

func (s statistics) TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error) {
	return s.store.TrafficLog().TopUsersTrafficByDay(ctx, date, limit)
}

func (s statistics) ServerTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.ServerTrafficRanking, error) {
	return s.store.TrafficLog().QueryServerTrafficRanking(ctx, start, end)
}

func (s statistics) UserTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.UserTrafficRanking, error) {
	return s.store.TrafficLog().QueryUserTrafficRanking(ctx, start, end)
}

func (s statistics) TrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error) {
	return s.store.TrafficLog().QueryTrafficLogDetails(ctx, filter)
}
