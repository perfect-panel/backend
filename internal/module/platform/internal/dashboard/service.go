// Package dashboard implements the admin console subdomain of the platform
// module: cross-domain reporting aggregates. Every foreign-domain access is a
// read through a port typed with the platform's own read models; the
// composition root adapts the owning modules' facades to them.
package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/redis/go-redis/v9"
)

// Read ports onto the billing, identity, support and network domains. Each
// lists only what the dashboard reads, so a fake has to implement exactly
// that and a new dependency fails to compile instead of panicking.

// OrderStatsReader reads the order and revenue totals.
type OrderStatsReader interface {
	QueryDateOrders(ctx context.Context, date time.Time) (readmodel.OrdersTotal, error)
	QueryMonthlyOrders(ctx context.Context, date time.Time) (readmodel.OrdersTotal, error)
	QueryTotalOrders(ctx context.Context) (readmodel.OrdersTotal, error)
	QueryDailyOrdersList(ctx context.Context, date time.Time) ([]readmodel.OrdersTotalWithDate, error)
	QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]readmodel.OrdersTotalWithDate, error)
	QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryTotalUserCounts(ctx context.Context) (int64, int64, error)
}

// UserStatsReader reads the registration counts.
type UserStatsReader interface {
	QueryRegisterUserTotal(ctx context.Context) (int64, error)
	QueryRegisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error)
	QueryRegisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error)
	QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]readmodel.UserStatisticsWithDate, error)
	QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]readmodel.UserStatisticsWithDate, error)
}

// TicketStatsReader reads the number of tickets waiting for a reply.
type TicketStatsReader interface {
	QueryWaitReplyTotal(ctx context.Context) (int64, error)
}

// NodeStatsReader reads the node inventory and online counts.
type NodeStatsReader interface {
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error)
	OnlineUserSubscribeGlobal(ctx context.Context) (int64, error)
	QueryServerList(ctx context.Context, ids []int64) ([]*readmodel.Server, error)
}

// TrafficStatsReader reads the traffic totals and rankings.
type TrafficStatsReader interface {
	QueryTrafficSummary(ctx context.Context, start, end time.Time) (*readmodel.TotalTraffic, error)
	TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]readmodel.ServerTrafficRanking, error)
	TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]readmodel.UserTrafficRanking, error)
}

// LogReader reads the archived daily statistics.
type LogReader interface {
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*log.SystemLog, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error)
}

// Cache is the dashboard's snapshot cache; the redis client satisfies it
// structurally.
type Cache interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Orders  OrderStatsReader
	Users   UserStatsReader
	Tickets TicketStatsReader
	Nodes   NodeStatsReader
	Traffic TrafficStatsReader
	Logs    LogReader
	Cache   Cache
}

// Service computes the admin console figures for the platform facade.
type Service struct {
	deps Deps
}

// NewService builds the dashboard service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// demoMode reports whether this is the demo deployment, which shows canned
// figures instead of real ones.
func demoMode() bool {
	return strings.ToLower(os.Getenv("PPANEL_MODE")) == "demo"
}

// readSnapshot returns the summary cached under key, if a readable one is
// there.
func readSnapshot[T any](ctx context.Context, cache Cache, key string) (*T, bool) {
	cached, err := cache.Get(ctx, key).Result()
	if err != nil || cached == "" {
		return nil, false
	}
	var snapshot T
	if json.Unmarshal([]byte(cached), &snapshot) != nil {
		return nil, false
	}
	return &snapshot, true
}

// storeSnapshot caches summary under key for ttl. Caching is best effort: a
// summary that is not cached is computed again on the next read.
func storeSnapshot(ctx context.Context, cache Cache, key string, summary any, ttl time.Duration) {
	data, err := json.Marshal(summary)
	if err != nil {
		return
	}
	cache.Set(ctx, key, data, ttl)
}
