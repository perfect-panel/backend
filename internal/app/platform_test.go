package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/support"
)

var errFacade = errors.New("facade unavailable")

// statisticsBilling answers every order figure with fixed values, or with err.
type statisticsBilling struct {
	err  error
	date time.Time
}

var _ billing.OrderStatistics = (*statisticsBilling)(nil)

func (b *statisticsBilling) OrderRevenueOn(_ context.Context, date time.Time) (order.OrdersTotal, error) {
	b.date = date
	return order.OrdersTotal{AmountTotal: 30, NewOrderAmount: 20, RenewalOrderAmount: 10}, b.err
}

func (b *statisticsBilling) OrderRevenueInMonth(_ context.Context, date time.Time) (order.OrdersTotal, error) {
	b.date = date
	return order.OrdersTotal{AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100}, b.err
}

func (b *statisticsBilling) OrderRevenueTotal(context.Context) (order.OrdersTotal, error) {
	return order.OrdersTotal{AmountTotal: 3000, NewOrderAmount: 2000, RenewalOrderAmount: 1000}, b.err
}

func (b *statisticsBilling) DailyOrderRevenue(_ context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	b.date = date
	return []order.OrdersTotalWithDate{{Date: "2026-09-01", AmountTotal: 3, NewOrderAmount: 2, RenewalOrderAmount: 1}}, b.err
}

func (b *statisticsBilling) MonthlyOrderRevenue(_ context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	b.date = date
	return nil, b.err
}

func (b *statisticsBilling) PayingUsersOn(_ context.Context, date time.Time) (int64, int64, error) {
	b.date = date
	return 4, 5, b.err
}

func (b *statisticsBilling) PayingUsersInMonth(_ context.Context, date time.Time) (int64, int64, error) {
	b.date = date
	return 40, 50, b.err
}

func (b *statisticsBilling) PayingUsersTotal(context.Context) (int64, int64, error) {
	return 400, 500, b.err
}

func TestPlatformOrdersConvertBillingFigures(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	source := &statisticsBilling{}
	orders := platformOrders{billing: func() billing.OrderStatistics { return source }}

	today, err := orders.QueryDateOrders(ctx, day)
	if err != nil || today != (platform.OrdersTotal{AmountTotal: 30, NewOrderAmount: 20, RenewalOrderAmount: 10}) || !source.date.Equal(day) {
		t.Fatalf("today = %+v, %v (asked for %v)", today, err, source.date)
	}
	month, err := orders.QueryMonthlyOrders(ctx, day)
	if err != nil || month != (platform.OrdersTotal{AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100}) {
		t.Fatalf("month = %+v, %v", month, err)
	}
	total, err := orders.QueryTotalOrders(ctx)
	if err != nil || total != (platform.OrdersTotal{AmountTotal: 3000, NewOrderAmount: 2000, RenewalOrderAmount: 1000}) {
		t.Fatalf("total = %+v, %v", total, err)
	}
	daily, err := orders.QueryDailyOrdersList(ctx, day)
	want := []platform.OrdersTotalWithDate{{Date: "2026-09-01", AmountTotal: 3, NewOrderAmount: 2, RenewalOrderAmount: 1}}
	if err != nil || !reflect.DeepEqual(daily, want) {
		t.Fatalf("daily = %+v, %v", daily, err)
	}
	if monthly, err := orders.QueryMonthlyOrdersList(ctx, day); err != nil || monthly != nil {
		t.Fatalf("monthly = %#v, %v; want the nil breakdown kept nil", monthly, err)
	}
	for _, tc := range []struct {
		name               string
		count              func() (int64, int64, error)
		newUsers, renewals int64
	}{
		{"today", func() (int64, int64, error) { return orders.QueryDateUserCounts(ctx, day) }, 4, 5},
		{"month", func() (int64, int64, error) { return orders.QueryMonthlyUserCounts(ctx, day) }, 40, 50},
		{"all", func() (int64, int64, error) { return orders.QueryTotalUserCounts(ctx) }, 400, 500},
	} {
		newUsers, renewals, err := tc.count()
		if err != nil || newUsers != tc.newUsers || renewals != tc.renewals {
			t.Fatalf("%s paying users = %d/%d, %v; want %d/%d", tc.name, newUsers, renewals, err, tc.newUsers, tc.renewals)
		}
	}

	source.err = errFacade
	if _, err := orders.QueryDateOrders(ctx, day); !errors.Is(err, errFacade) {
		t.Fatalf("error = %v, want the facade's", err)
	}
	if _, _, err := orders.QueryTotalUserCounts(ctx); !errors.Is(err, errFacade) {
		t.Fatalf("error = %v, want the facade's", err)
	}
}

// statisticsIdentity answers the platform's identity reads with fixed values.
type statisticsIdentity struct {
	err     error
	methods []*auth.Auth
}

var _ platformIdentity = (*statisticsIdentity)(nil)

func (i *statisticsIdentity) CountRegisteredUsers(context.Context) (int64, error) { return 100, i.err }
func (i *statisticsIdentity) CountRegisteredUsersOn(context.Context, time.Time) (int64, error) {
	return 1, i.err
}
func (i *statisticsIdentity) CountRegisteredUsersInMonth(context.Context, time.Time) (int64, error) {
	return 10, i.err
}
func (i *statisticsIdentity) DailyUserStatistics(context.Context, time.Time) ([]user.UserStatisticsWithDate, error) {
	return []user.UserStatisticsWithDate{{Date: "2026-09-01", Register: 2, NewOrderUsers: 1, RenewalOrderUsers: 3}}, i.err
}
func (i *statisticsIdentity) MonthlyUserStatistics(context.Context, time.Time) ([]user.UserStatisticsWithDate, error) {
	return []user.UserStatisticsWithDate{}, i.err
}
func (i *statisticsIdentity) ListLoginMethods(context.Context) ([]*auth.Auth, error) {
	return i.methods, i.err
}
func (i *statisticsIdentity) CountEnabledUsers(context.Context) (int64, error) { return 42, i.err }

func TestPlatformUsersConvertIdentityFigures(t *testing.T) {
	ctx := context.Background()
	enabled, disabled := true, false
	source := &statisticsIdentity{methods: []*auth.Auth{
		{Id: 1, Method: "email", Config: `{"enable":true}`, Enabled: &enabled},
		{Id: 2, Method: "device", Config: `{"show_ads":true}`, Enabled: &disabled},
		{Id: 3, Method: "mobile"},
	}}
	users := platformUsers{identity: func() platformIdentity { return source }}

	counts := []func() (int64, error){
		func() (int64, error) { return users.QueryRegisterUserTotal(ctx) },
		func() (int64, error) { return users.QueryRegisterUserTotalByDate(ctx, time.Now()) },
		func() (int64, error) { return users.QueryRegisterUserTotalByMonthly(ctx, time.Now()) },
		func() (int64, error) { return users.CountEnabledUsers(ctx) },
	}
	for i, want := range []int64{100, 1, 10, 42} {
		if got, err := counts[i](); err != nil || got != want {
			t.Fatalf("count %d = %d, %v; want %d", i, got, err, want)
		}
	}
	daily, err := users.QueryDailyUserStatisticsList(ctx, time.Now())
	want := []platform.UserStatisticsWithDate{{Date: "2026-09-01", Register: 2, NewOrderUsers: 1, RenewalOrderUsers: 3}}
	if err != nil || !reflect.DeepEqual(daily, want) {
		t.Fatalf("daily = %+v, %v", daily, err)
	}
	if monthly, err := users.QueryMonthlyUserStatisticsList(ctx, time.Now()); err != nil || monthly == nil || len(monthly) != 0 {
		t.Fatalf("monthly = %#v, %v; want the empty breakdown kept empty", monthly, err)
	}
	methods, err := users.ListAuthMethods(ctx)
	wantMethods := []platform.AuthMethod{
		{Method: "email", Config: `{"enable":true}`, Enabled: true},
		{Method: "device", Config: `{"show_ads":true}`, Enabled: false},
		{Method: "mobile", Enabled: false},
	}
	if err != nil || !reflect.DeepEqual(methods, wantMethods) {
		t.Fatalf("methods = %+v, %v", methods, err)
	}

	source.err, source.methods = errFacade, nil
	if methods, err := users.ListAuthMethods(ctx); !errors.Is(err, errFacade) || methods != nil {
		t.Fatalf("methods = %#v, %v; want nil and the facade's error", methods, err)
	}
}

// statisticsTickets counts the tickets awaiting a reply.
type statisticsTickets struct{}

var _ support.TicketStatistics = statisticsTickets{}

func (statisticsTickets) CountTicketsAwaitingReply(context.Context) (int64, error) { return 7, nil }

func TestPlatformTicketsReadSupport(t *testing.T) {
	tickets := platformTickets{support: func() support.TicketStatistics { return statisticsTickets{} }}
	if count, err := tickets.QueryWaitReplyTotal(context.Background()); err != nil || count != 7 {
		t.Fatalf("count = %d, %v", count, err)
	}
}

// statisticsNetwork answers the platform's network reads with fixed values
// and records the details filter it was asked for.
type statisticsNetwork struct {
	err    error
	total  *traffic.TotalTraffic
	filter *traffic.TrafficLogDetailsFilter
}

var _ network.Statistics = (*statisticsNetwork)(nil)

func (n *statisticsNetwork) CountEnabledNodes(context.Context) (int64, error) { return 9, n.err }
func (n *statisticsNetwork) ListServerAddresses(context.Context) ([]string, error) {
	return []string{"hk.example", "1.0.0.1"}, n.err
}
func (n *statisticsNetwork) ListEnabledNodeProtocols(context.Context) ([]string, error) {
	return []string{"vless", "trojan"}, n.err
}
func (n *statisticsNetwork) CountServersByReportStatus(context.Context, time.Time) (int64, int64, error) {
	return 3, 2, n.err
}
func (n *statisticsNetwork) CountOnlineUsers(context.Context) (int64, error) { return 11, n.err }
func (n *statisticsNetwork) FindServers(_ context.Context, ids []int64) ([]*node.Server, error) {
	servers := make([]*node.Server, 0, len(ids))
	for _, id := range ids {
		servers = append(servers, &node.Server{Id: id, Name: "server", Address: "hidden"})
	}
	return servers, n.err
}
func (n *statisticsNetwork) TrafficSummary(context.Context, time.Time, time.Time) (*traffic.TotalTraffic, error) {
	return n.total, n.err
}
func (n *statisticsNetwork) TopServersTrafficByDay(context.Context, time.Time, int) ([]traffic.ServerTrafficRanking, error) {
	return []traffic.ServerTrafficRanking{{ServerId: 1, Download: 2, Upload: 3, Total: 5}}, n.err
}
func (n *statisticsNetwork) TopUsersTrafficByDay(context.Context, time.Time, int) ([]traffic.UserTrafficRanking, error) {
	return []traffic.UserTrafficRanking{{UserId: 4, SubscribeId: 5, Download: 6, Upload: 7, Total: 13}}, n.err
}
func (n *statisticsNetwork) ServerTrafficRanking(context.Context, time.Time, time.Time) ([]traffic.ServerTrafficRanking, error) {
	return nil, n.err
}
func (n *statisticsNetwork) UserTrafficRanking(context.Context, time.Time, time.Time) ([]traffic.UserTrafficRanking, error) {
	return []traffic.UserTrafficRanking{{UserId: 8, SubscribeId: 9, Download: 1, Upload: 1, Total: 2}}, n.err
}
func (n *statisticsNetwork) TrafficLogDetails(_ context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error) {
	n.filter = filter
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	return []*traffic.TrafficLog{{Id: 1, ServerId: 2, UserId: 3, SubscribeId: 4, Download: 5, Upload: 6, Timestamp: at}, nil}, 12, n.err
}

func TestPlatformNetworkConvertsNodeAndTrafficFigures(t *testing.T) {
	ctx := context.Background()
	source := &statisticsNetwork{total: &traffic.TotalTraffic{Download: 200, Upload: 100}}
	reads := platformNetwork{network: func() network.Statistics { return source }}

	if count, err := reads.CountEnabledNodes(ctx); err != nil || count != 9 {
		t.Fatalf("nodes = %d, %v", count, err)
	}
	if addresses, err := reads.QueryServerAddresses(ctx); err != nil || !reflect.DeepEqual(addresses, []string{"hk.example", "1.0.0.1"}) {
		t.Fatalf("addresses = %v, %v", addresses, err)
	}
	if protocols, err := reads.QueryEnabledNodeProtocols(ctx); err != nil || !reflect.DeepEqual(protocols, []string{"vless", "trojan"}) {
		t.Fatalf("protocols = %v, %v", protocols, err)
	}
	if online, offline, err := reads.CountServersByReportStatus(ctx, time.Now()); err != nil || online != 3 || offline != 2 {
		t.Fatalf("servers = %d/%d, %v", online, offline, err)
	}
	if users, err := reads.OnlineUserSubscribeGlobal(ctx); err != nil || users != 11 {
		t.Fatalf("online users = %d, %v", users, err)
	}
	servers, err := reads.QueryServerList(ctx, []int64{1, 2})
	if err != nil || !reflect.DeepEqual(servers, []*platform.Server{{Id: 1, Name: "server"}, {Id: 2, Name: "server"}}) {
		t.Fatalf("servers = %+v, %v", servers, err)
	}

	total, err := reads.QueryTrafficSummary(ctx, time.Now(), time.Now())
	if err != nil || total == nil || *total != (platform.TotalTraffic{Download: 200, Upload: 100}) {
		t.Fatalf("summary = %+v, %v", total, err)
	}
	topServers, err := reads.TopServersTrafficByDay(ctx, time.Now(), 10)
	if err != nil || !reflect.DeepEqual(topServers, []platform.ServerTrafficRanking{{ServerId: 1, Download: 2, Upload: 3, Total: 5}}) {
		t.Fatalf("top servers = %+v, %v", topServers, err)
	}
	topUsers, err := reads.TopUsersTrafficByDay(ctx, time.Now(), 10)
	if err != nil || !reflect.DeepEqual(topUsers, []platform.UserTrafficRanking{{UserId: 4, SubscribeId: 5, Download: 6, Upload: 7, Total: 13}}) {
		t.Fatalf("top users = %+v, %v", topUsers, err)
	}
	if ranking, err := reads.QueryServerTrafficRanking(ctx, time.Now(), time.Now()); err != nil || ranking != nil {
		t.Fatalf("server ranking = %#v, %v; want the nil ranking kept nil", ranking, err)
	}
	userRanking, err := reads.QueryUserTrafficRanking(ctx, time.Now(), time.Now())
	if err != nil || !reflect.DeepEqual(userRanking, []platform.UserTrafficRanking{{UserId: 8, SubscribeId: 9, Download: 1, Upload: 1, Total: 2}}) {
		t.Fatalf("user ranking = %+v, %v", userRanking, err)
	}

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	filter := &platform.TrafficLogDetailsFilter{ServerId: 1, UserId: 2, SubscribeId: 3, Start: start, End: start.AddDate(0, 0, 1), Page: 4, Size: 5}
	entries, count, err := reads.QueryTrafficLogDetails(ctx, filter)
	wantFilter := &traffic.TrafficLogDetailsFilter{ServerId: 1, UserId: 2, SubscribeId: 3, Start: start, End: start.AddDate(0, 0, 1), Page: 4, Size: 5}
	if !reflect.DeepEqual(source.filter, wantFilter) {
		t.Fatalf("filter = %+v, want %+v", source.filter, wantFilter)
	}
	wantEntries := []*platform.TrafficLog{{Id: 1, ServerId: 2, UserId: 3, SubscribeId: 4, Download: 5, Upload: 6, Timestamp: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}, nil}
	if err != nil || count != 12 || !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("entries = %+v (total %d), %v", entries, count, err)
	}
	if _, _, err := reads.QueryTrafficLogDetails(ctx, nil); err != nil || source.filter != nil {
		t.Fatalf("a nil filter reached network as %+v (%v), want nil", source.filter, err)
	}

	source.total, source.err = nil, errFacade
	if total, err := reads.QueryTrafficSummary(ctx, time.Now(), time.Now()); total != nil || !errors.Is(err, errFacade) {
		t.Fatalf("summary = %+v, %v; want nil and the facade's error", total, err)
	}
	if _, err := reads.QueryServerList(ctx, []int64{1}); !errors.Is(err, errFacade) {
		t.Fatalf("error = %v, want the facade's", err)
	}
}
