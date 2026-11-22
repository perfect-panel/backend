package app

import (
	"context"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/repository"
)

// newPlatformModule wires the platform module against the application store;
// the callbacks read and update the running configuration. The platform reads
// the other domains through the facade adapters below.
func newPlatformModule(store repository.Store, srv *Application) platform.Service {
	networkReads := platformNetwork{network: func() network.Statistics { return srv.Network }}
	return platform.New(platform.Deps{
		Logs:    store.Log(),
		System:  store.System(),
		Traffic: networkReads,
		Store:   store,
		Orders:  platformOrders{billing: func() billing.OrderStatistics { return srv.Billing }},
		Users:   platformUsers{identity: func() platformIdentity { return srv.Identity }},
		Tickets: platformTickets{support: func() support.TicketStatistics { return srv.Support }},
		Nodes:   networkReads,
		Cache:   srv.Redis,
		OnLogSettingChanged: func(autoClear bool, clearDays int64) {
			srv.Runtime.UpdateRuntime(func(current *config.Runtime) {
				current.Log = config.Log{AutoClear: autoClear, ClearDays: clearDays}
			})
		},
		LogRetention: func() (bool, int64) {
			current := srv.Runtime.Config().Log
			return current.AutoClear, current.ClearDays
		},
		TrafficRetention: platformTrafficRetention{srv},
		Reinitialize:     srv.Runtime.Reinitialize,
		Restart:          srv.Runtime.Restart,
		SubscribePath: func() string {
			return srv.Runtime.Config().Subscribe.SubscribePath
		},
		Multiplier: func(at time.Time) float32 {
			manager := srv.Runtime.NodeMultiplierManager()
			if manager == nil {
				return 1
			}
			return manager.GetMultiplier(at)
		},
		PublicStore: store,
		Redis:       srv.Redis,
		PublicConfig: func() platform.GlobalConfigSnapshot {
			c := srv.Runtime.Config()
			return platform.GlobalConfigSnapshot{
				Site:      c.Site,
				Subscribe: c.Subscribe,
				Email:     c.Email,
				Mobile:    c.Mobile,
				Register:  c.Register,
				Verify:    c.Verify,
				Invite:    c.Invite,
			}
		},
		Clients: platformClientApplications{srv},
		LogPath: srv.Runtime.Config().Logger.Path,
		GeoIP: func() *geoip2.Reader {
			if srv.GeoIP == nil {
				return nil
			}
			return srv.GeoIP.DB
		},
	})
}

// The platform module reads the other domains through ports typed with its
// own read models; the adapters below back them with the owning modules'
// facades and convert the facades' entity-typed results. They resolve the
// facades per call: platform is built before identity and network (see
// NewApplication). Results and errors pass through unchanged, and a nil
// slice stays nil.

// platformOrders backs the dashboard's order figures with billing.
type platformOrders struct {
	billing func() billing.OrderStatistics
}

var _ platform.OrderReader = platformOrders{}

func (o platformOrders) QueryDateOrders(ctx context.Context, date time.Time) (platform.OrdersTotal, error) {
	total, err := o.billing().OrderRevenueOn(ctx, date)
	return platformOrdersTotal(total), err
}

func (o platformOrders) QueryMonthlyOrders(ctx context.Context, date time.Time) (platform.OrdersTotal, error) {
	total, err := o.billing().OrderRevenueInMonth(ctx, date)
	return platformOrdersTotal(total), err
}

func (o platformOrders) QueryTotalOrders(ctx context.Context) (platform.OrdersTotal, error) {
	total, err := o.billing().OrderRevenueTotal(ctx)
	return platformOrdersTotal(total), err
}

func (o platformOrders) QueryDailyOrdersList(ctx context.Context, date time.Time) ([]platform.OrdersTotalWithDate, error) {
	periods, err := o.billing().DailyOrderRevenue(ctx, date)
	return convertAll(periods, platformDatedOrdersTotal), err
}

func (o platformOrders) QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]platform.OrdersTotalWithDate, error) {
	periods, err := o.billing().MonthlyOrderRevenue(ctx, date)
	return convertAll(periods, platformDatedOrdersTotal), err
}

func (o platformOrders) QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	return o.billing().PayingUsersOn(ctx, date)
}

func (o platformOrders) QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	return o.billing().PayingUsersInMonth(ctx, date)
}

func (o platformOrders) QueryTotalUserCounts(ctx context.Context) (int64, int64, error) {
	return o.billing().PayingUsersTotal(ctx)
}

func platformOrdersTotal(total order.OrdersTotal) platform.OrdersTotal {
	return platform.OrdersTotal{
		AmountTotal:        total.AmountTotal,
		NewOrderAmount:     total.NewOrderAmount,
		RenewalOrderAmount: total.RenewalOrderAmount,
	}
}

func platformDatedOrdersTotal(period order.OrdersTotalWithDate) platform.OrdersTotalWithDate {
	return platform.OrdersTotalWithDate{
		Date:               period.Date,
		AmountTotal:        period.AmountTotal,
		NewOrderAmount:     period.NewOrderAmount,
		RenewalOrderAmount: period.RenewalOrderAmount,
	}
}

// platformIdentity is the part of the identity facade the platform reads.
type platformIdentity interface {
	CountRegisteredUsers(ctx context.Context) (int64, error)
	CountRegisteredUsersOn(ctx context.Context, day time.Time) (int64, error)
	CountRegisteredUsersInMonth(ctx context.Context, month time.Time) (int64, error)
	DailyUserStatistics(ctx context.Context, until time.Time) ([]user.UserStatisticsWithDate, error)
	MonthlyUserStatistics(ctx context.Context, date time.Time) ([]user.UserStatisticsWithDate, error)
	ListLoginMethods(ctx context.Context) ([]*auth.Auth, error)
	CountEnabledUsers(ctx context.Context) (int64, error)
}

// platformUsers backs the dashboard's registration figures and the public
// site's login methods and account count with identity.
type platformUsers struct {
	identity func() platformIdentity
}

var _ platform.UserReader = platformUsers{}

func (u platformUsers) QueryRegisterUserTotal(ctx context.Context) (int64, error) {
	return u.identity().CountRegisteredUsers(ctx)
}

func (u platformUsers) QueryRegisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	return u.identity().CountRegisteredUsersOn(ctx, date)
}

func (u platformUsers) QueryRegisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	return u.identity().CountRegisteredUsersInMonth(ctx, date)
}

func (u platformUsers) QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]platform.UserStatisticsWithDate, error) {
	periods, err := u.identity().DailyUserStatistics(ctx, date)
	return convertAll(periods, platformUserStatistics), err
}

func (u platformUsers) QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]platform.UserStatisticsWithDate, error) {
	periods, err := u.identity().MonthlyUserStatistics(ctx, date)
	return convertAll(periods, platformUserStatistics), err
}

func (u platformUsers) ListAuthMethods(ctx context.Context) ([]platform.AuthMethod, error) {
	methods, err := u.identity().ListLoginMethods(ctx)
	return convertAll(methods, platformAuthMethod), err
}

func (u platformUsers) CountEnabledUsers(ctx context.Context) (int64, error) {
	return u.identity().CountEnabledUsers(ctx)
}

func platformUserStatistics(period user.UserStatisticsWithDate) platform.UserStatisticsWithDate {
	return platform.UserStatisticsWithDate{
		Date:              period.Date,
		Register:          period.Register,
		NewOrderUsers:     period.NewOrderUsers,
		RenewalOrderUsers: period.RenewalOrderUsers,
	}
}

// platformAuthMethod reads a missing enabled flag as disabled; the column is
// NOT NULL, so a stored method always carries one.
func platformAuthMethod(method *auth.Auth) platform.AuthMethod {
	if method == nil {
		return platform.AuthMethod{}
	}
	return platform.AuthMethod{
		Method:  method.Method,
		Config:  method.Config,
		Enabled: method.Enabled != nil && *method.Enabled,
	}
}

// platformTickets backs the dashboard's ticket count with support.
type platformTickets struct {
	support func() support.TicketStatistics
}

var _ platform.TicketReader = platformTickets{}

func (t platformTickets) QueryWaitReplyTotal(ctx context.Context) (int64, error) {
	return t.support().CountTicketsAwaitingReply(ctx)
}

// platformTrafficRetention serves the log retention's pruning of the raw
// traffic log from the network facade, which is constructed after platform
// and resolved per call.
type platformTrafficRetention struct{ srv *Application }

func (r platformTrafficRetention) PruneTrafficLogs(ctx context.Context, before time.Time) (int64, error) {
	return r.srv.Network.PruneTrafficLogs(ctx, before)
}

// platformNetwork backs the node and traffic figures of the dashboard, the
// audit views and the public statistics with network.
type platformNetwork struct {
	network func() network.Statistics
}

var (
	_ platform.NodeReader    = platformNetwork{}
	_ platform.TrafficReader = platformNetwork{}
)

func (n platformNetwork) CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error) {
	return n.network().CountServersByReportStatus(ctx, cutoff)
}

func (n platformNetwork) OnlineUserSubscribeGlobal(ctx context.Context) (int64, error) {
	return n.network().CountOnlineUsers(ctx)
}

func (n platformNetwork) QueryServerList(ctx context.Context, ids []int64) ([]*platform.Server, error) {
	servers, err := n.network().FindServers(ctx, ids)
	return convertAll(servers, platformServer), err
}

func (n platformNetwork) CountEnabledNodes(ctx context.Context) (int64, error) {
	return n.network().CountEnabledNodes(ctx)
}

func (n platformNetwork) QueryServerAddresses(ctx context.Context) ([]string, error) {
	return n.network().ListServerAddresses(ctx)
}

func (n platformNetwork) QueryEnabledNodeProtocols(ctx context.Context) ([]string, error) {
	return n.network().ListEnabledNodeProtocols(ctx)
}

func (n platformNetwork) QueryTrafficSummary(ctx context.Context, start, end time.Time) (*platform.TotalTraffic, error) {
	total, err := n.network().TrafficSummary(ctx, start, end)
	if total == nil {
		return nil, err
	}
	return &platform.TotalTraffic{Download: total.Download, Upload: total.Upload}, err
}

func (n platformNetwork) TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]platform.ServerTrafficRanking, error) {
	ranking, err := n.network().TopServersTrafficByDay(ctx, date, limit)
	return convertAll(ranking, platformServerTraffic), err
}

func (n platformNetwork) TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]platform.UserTrafficRanking, error) {
	ranking, err := n.network().TopUsersTrafficByDay(ctx, date, limit)
	return convertAll(ranking, platformUserTraffic), err
}

func (n platformNetwork) QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]platform.ServerTrafficRanking, error) {
	ranking, err := n.network().ServerTrafficRanking(ctx, start, end)
	return convertAll(ranking, platformServerTraffic), err
}

func (n platformNetwork) QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]platform.UserTrafficRanking, error) {
	ranking, err := n.network().UserTrafficRanking(ctx, start, end)
	return convertAll(ranking, platformUserTraffic), err
}

func (n platformNetwork) QueryTrafficLogDetails(ctx context.Context, filter *platform.TrafficLogDetailsFilter) ([]*platform.TrafficLog, int64, error) {
	var query *traffic.TrafficLogDetailsFilter
	if filter != nil {
		query = &traffic.TrafficLogDetailsFilter{
			ServerId:    filter.ServerId,
			UserId:      filter.UserId,
			SubscribeId: filter.SubscribeId,
			Start:       filter.Start,
			End:         filter.End,
			Page:        filter.Page,
			Size:        filter.Size,
		}
	}
	entries, total, err := n.network().TrafficLogDetails(ctx, query)
	return convertAll(entries, platformTrafficLog), total, err
}

func platformServer(server *node.Server) *platform.Server {
	if server == nil {
		return nil
	}
	return &platform.Server{Id: server.Id, Name: server.Name}
}

func platformServerTraffic(item traffic.ServerTrafficRanking) platform.ServerTrafficRanking {
	return platform.ServerTrafficRanking{
		ServerId: item.ServerId,
		Download: item.Download,
		Upload:   item.Upload,
		Total:    item.Total,
	}
}

func platformUserTraffic(item traffic.UserTrafficRanking) platform.UserTrafficRanking {
	return platform.UserTrafficRanking{
		UserId:      item.UserId,
		SubscribeId: item.SubscribeId,
		Download:    item.Download,
		Upload:      item.Upload,
		Total:       item.Total,
	}
}

func platformTrafficLog(entry *traffic.TrafficLog) *platform.TrafficLog {
	if entry == nil {
		return nil
	}
	return &platform.TrafficLog{
		Id:          entry.Id,
		ServerId:    entry.ServerId,
		UserId:      entry.UserId,
		SubscribeId: entry.SubscribeId,
		Download:    entry.Download,
		Upload:      entry.Upload,
		Timestamp:   entry.Timestamp,
	}
}

// convertAll maps items with convert, keeping a nil slice nil.
func convertAll[S, T any](items []S, convert func(S) T) []T {
	if items == nil {
		return nil
	}
	converted := make([]T, len(items))
	for i, item := range items {
		converted[i] = convert(item)
	}
	return converted
}
