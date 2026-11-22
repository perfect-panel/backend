package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var errBackend = errors.New("backend unavailable")

var (
	_ TrafficStatsReader = (*fakeTraffic)(nil)
	_ NodeStatsReader    = (*fakeNodes)(nil)
	_ OrderStatsReader   = (*fakeOrders)(nil)
	_ UserStatsReader    = (*fakeUsers)(nil)
	_ TicketStatsReader  = (*fakeTickets)(nil)
)

type fakeTraffic struct {
	users                          []readmodel.UserTrafficRanking
	servers                        []readmodel.ServerTrafficRanking
	total                          readmodel.TotalTraffic
	usersErr, serversErr, totalErr error
}

func (f *fakeTraffic) TopUsersTrafficByDay(context.Context, time.Time, int) ([]readmodel.UserTrafficRanking, error) {
	return f.users, f.usersErr
}

func (f *fakeTraffic) TopServersTrafficByDay(context.Context, time.Time, int) ([]readmodel.ServerTrafficRanking, error) {
	return f.servers, f.serversErr
}

func (f *fakeTraffic) QueryTrafficSummary(context.Context, time.Time, time.Time) (*readmodel.TotalTraffic, error) {
	if f.totalErr != nil {
		return nil, f.totalErr
	}
	total := f.total
	return &total, nil
}

type fakeNodes struct {
	names                        map[int64]string
	online, up, down             int64
	listErr, onlineErr, countErr error
}

func (f *fakeNodes) QueryServerList(_ context.Context, ids []int64) ([]*readmodel.Server, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var servers []*readmodel.Server
	for _, id := range ids {
		if name, ok := f.names[id]; ok {
			servers = append(servers, &readmodel.Server{Id: id, Name: name})
		}
	}
	return servers, nil
}

func (f *fakeNodes) OnlineUserSubscribeGlobal(context.Context) (int64, error) {
	return f.online, f.onlineErr
}

func (f *fakeNodes) CountServersByReportStatus(context.Context, time.Time) (int64, int64, error) {
	return f.up, f.down, f.countErr
}

type fakeOrders struct {
	today, month, total                  readmodel.OrdersTotal
	daily, monthly                       []readmodel.OrdersTotalWithDate
	todayErr, monthErr, totalErr         error
	dailyErr, monthlyErr                 error
	newToday, renewToday                 int64
	newMonth, renewMonth                 int64
	newAll, renewAll                     int64
	todayUsersErr, monthUsersErr, allErr error
}

func (f *fakeOrders) QueryDateOrders(context.Context, time.Time) (readmodel.OrdersTotal, error) {
	return f.today, f.todayErr
}

func (f *fakeOrders) QueryMonthlyOrders(context.Context, time.Time) (readmodel.OrdersTotal, error) {
	return f.month, f.monthErr
}

func (f *fakeOrders) QueryTotalOrders(context.Context) (readmodel.OrdersTotal, error) {
	return f.total, f.totalErr
}

func (f *fakeOrders) QueryDailyOrdersList(context.Context, time.Time) ([]readmodel.OrdersTotalWithDate, error) {
	return f.daily, f.dailyErr
}

func (f *fakeOrders) QueryMonthlyOrdersList(context.Context, time.Time) ([]readmodel.OrdersTotalWithDate, error) {
	return f.monthly, f.monthlyErr
}

func (f *fakeOrders) QueryDateUserCounts(context.Context, time.Time) (int64, int64, error) {
	return f.newToday, f.renewToday, f.todayUsersErr
}

func (f *fakeOrders) QueryMonthlyUserCounts(context.Context, time.Time) (int64, int64, error) {
	return f.newMonth, f.renewMonth, f.monthUsersErr
}

func (f *fakeOrders) QueryTotalUserCounts(context.Context) (int64, int64, error) {
	return f.newAll, f.renewAll, f.allErr
}

type fakeUsers struct {
	today, month, total int64
	daily, monthly      []readmodel.UserStatisticsWithDate
	err                 error
}

func (f *fakeUsers) QueryRegisterUserTotalByDate(context.Context, time.Time) (int64, error) {
	return f.today, f.err
}

func (f *fakeUsers) QueryRegisterUserTotalByMonthly(context.Context, time.Time) (int64, error) {
	return f.month, f.err
}

func (f *fakeUsers) QueryRegisterUserTotal(context.Context) (int64, error) {
	return f.total, f.err
}

func (f *fakeUsers) QueryDailyUserStatisticsList(context.Context, time.Time) ([]readmodel.UserStatisticsWithDate, error) {
	return f.daily, f.err
}

func (f *fakeUsers) QueryMonthlyUserStatisticsList(context.Context, time.Time) ([]readmodel.UserStatisticsWithDate, error) {
	return f.monthly, f.err
}

type fakeTickets struct {
	waiting int64
	err     error
}

func (f *fakeTickets) QueryWaitReplyTotal(context.Context) (int64, error) {
	return f.waiting, f.err
}

type world struct {
	svc     *Service
	db      *gorm.DB
	redis   *miniredis.Miniredis
	traffic *fakeTraffic
	nodes   *fakeNodes
	orders  *fakeOrders
	users   *fakeUsers
	tickets *fakeTickets
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:dashboard-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	w := &world{
		db:      db,
		redis:   server,
		traffic: &fakeTraffic{},
		nodes:   &fakeNodes{names: map[int64]string{}},
		orders:  &fakeOrders{},
		users:   &fakeUsers{},
		tickets: &fakeTickets{},
	}
	w.svc = NewService(Deps{
		Orders:  w.orders,
		Users:   w.users,
		Tickets: w.tickets,
		Nodes:   w.nodes,
		Traffic: w.traffic,
		Logs:    repo.NewLogRepo(db),
		Cache:   rds,
	})
	return w
}

type storedLog interface{ Marshal() ([]byte, error) }

func (w *world) addLog(t *testing.T, typ log.Type, date string, entry storedLog) {
	t.Helper()
	content, err := entry.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	w.addRawLog(t, typ, date, string(content))
}

func (w *world) addRawLog(t *testing.T, typ log.Type, date, content string) {
	t.Helper()
	if err := w.db.Create(&log.SystemLog{Type: typ.Uint8(), Date: date, Content: content}).Error; err != nil {
		t.Fatal(err)
	}
}

// The dates the dashboard reads, computed the way it computes them.
type calendar struct {
	now        time.Time
	today      string
	yesterday  string
	earlierDay []string // the days of this month before today
	lastMonth  string
}

func today() calendar {
	return calendarAt(timeutil.Now())
}

// calendarAt is the calendar of the day of now. Yesterday is one calendar
// day back (AddDate), as the dashboard computes it: on the day after clocks
// went forward, 24 hours back lands two days back.
func calendarAt(now time.Time) calendar {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	c := calendar{
		now:       now,
		today:     start.Format(time.DateOnly),
		yesterday: start.AddDate(0, 0, -1).Format(time.DateOnly),
		lastMonth: time.Date(now.Year(), now.Month(), 0, 0, 0, 0, 0, now.Location()).Format(time.DateOnly),
	}
	for day := 1; day < now.Day(); day++ {
		c.earlierDay = append(c.earlierDay, time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location()).Format(time.DateOnly))
	}
	return c
}

// On the day after clocks went forward, yesterday is still the calendar day
// before: the archive must be seeded on the date the dashboard reads.
func TestCalendarYesterdayAcrossDaylightSaving(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	// Clocks went forward on 2026-03-29.
	cal := calendarAt(time.Date(2026, time.March, 30, 12, 0, 0, 0, berlin))
	if cal.today != "2026-03-30" || cal.yesterday != "2026-03-29" {
		t.Fatalf("today %s, yesterday %s; want 2026-03-30 and 2026-03-29", cal.today, cal.yesterday)
	}
}

func TestServerTotalDataCombinesLiveTrafficWithArchivedRankings(t *testing.T) {
	w := newWorld(t)
	cal := today()
	w.traffic.users = []readmodel.UserTrafficRanking{
		{UserId: 7, SubscribeId: 70, Upload: 1, Download: 2, Total: 3},
		{UserId: 8, SubscribeId: 80, Upload: 3, Download: 4, Total: 7},
	}
	w.traffic.servers = []readmodel.ServerTrafficRanking{
		{ServerId: 1, Upload: 10, Download: 20, Total: 30},
		{ServerId: 4, Upload: 5, Download: 5, Total: 10}, // deleted since: no name
	}
	w.traffic.total = readmodel.TotalTraffic{Upload: 100, Download: 200}
	w.nodes.names = map[int64]string{1: "hk-01", 2: "jp-01", 3: "us-01"}
	w.nodes.online, w.nodes.up, w.nodes.down = 12, 2, 1

	// Yesterday's rankings, archived as rank logs keyed by position.
	w.addLog(t, log.TypeUserTrafficRank, cal.yesterday, &log.UserTrafficRank{Rank: map[uint8]log.UserTraffic{
		2: {SubscribeId: 90, UserId: 9, Upload: 5, Download: 6},
		1: {SubscribeId: 70, UserId: 7, Upload: 7, Download: 8},
	}})
	w.addLog(t, log.TypeServerTrafficRank, cal.yesterday, &log.ServerTrafficRank{Rank: map[uint8]log.ServerTraffic{
		2: {ServerId: 1, Upload: 1, Download: 1},
		1: {ServerId: 3, Upload: 2, Download: 2},
	}})
	// Archived daily totals: every earlier day of this month counts; today
	// comes from the live summary and last month is out of range.
	for _, day := range cal.earlierDay {
		w.addLog(t, log.TypeTrafficStat, day, &log.TrafficStat{Upload: 1000, Download: 2000})
	}
	if len(cal.earlierDay) > 0 {
		w.addRawLog(t, log.TypeTrafficStat, cal.earlierDay[0], "{corrupt")
	}
	w.addLog(t, log.TypeTrafficStat, cal.today, &log.TrafficStat{Upload: 1 << 40, Download: 1 << 40})
	w.addLog(t, log.TypeTrafficStat, cal.lastMonth, &log.TrafficStat{Upload: 1 << 41, Download: 1 << 41})

	got, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAt < cal.now.Unix() || got.UpdatedAt > time.Now().Unix()+1 {
		t.Fatalf("updated_at = %d, want the time of the query", got.UpdatedAt)
	}
	earlier := int64(len(cal.earlierDay))
	want := &dto.ServerTotalDataResponse{
		OnlineUsers:     12,
		OnlineServers:   2,
		OfflineServers:  1,
		TodayUpload:     100,
		TodayDownload:   200,
		MonthlyUpload:   100 + 1000*earlier,
		MonthlyDownload: 200 + 2000*earlier,
		UpdatedAt:       got.UpdatedAt,
		ServerTrafficRankingToday: []dto.ServerTrafficData{
			{ServerId: 1, Name: "hk-01", Upload: 10, Download: 20},
			{ServerId: 4, Name: "", Upload: 5, Download: 5},
		},
		ServerTrafficRankingYesterday: []dto.ServerTrafficData{
			{ServerId: 3, Name: "us-01", Upload: 2, Download: 2},
			{ServerId: 1, Name: "hk-01", Upload: 1, Download: 1},
		},
		UserTrafficRankingToday: []dto.UserTrafficData{
			{SID: 70, UID: 7, Upload: 1, Download: 2},
			{SID: 80, UID: 8, Upload: 3, Download: 4},
		},
		UserTrafficRankingYesterday: []dto.UserTrafficData{
			{SID: 70, UID: 7, Upload: 7, Download: 8},
			{SID: 90, UID: 9, Upload: 5, Download: 6},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("server total data\n%+v\nwant\n%+v", got, want)
	}
}

func TestServerTotalDataWithoutHistory(t *testing.T) {
	w := newWorld(t)
	w.traffic.total = readmodel.TotalTraffic{Upload: 1, Download: 2}
	got, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerTrafficRankingToday != nil || got.ServerTrafficRankingYesterday != nil ||
		got.UserTrafficRankingToday != nil || got.UserTrafficRankingYesterday != nil {
		t.Fatalf("rankings without traffic = %+v, want none", got)
	}
	if got.TodayUpload != 1 || got.MonthlyUpload != 1 || got.TodayDownload != 2 || got.MonthlyDownload != 2 {
		t.Fatalf("totals = %+v", got)
	}
}

// A corrupt archive or an unavailable server directory degrades the
// rankings instead of failing the dashboard.
func TestServerTotalDataToleratesDegradedSources(t *testing.T) {
	w := newWorld(t)
	cal := today()
	w.traffic.servers = []readmodel.ServerTrafficRanking{{ServerId: 1, Upload: 1, Download: 1}}
	w.nodes.names = map[int64]string{1: "hk-01"}
	w.nodes.listErr = errBackend
	w.addRawLog(t, log.TypeUserTrafficRank, cal.yesterday, "{corrupt")
	w.addRawLog(t, log.TypeServerTrafficRank, cal.yesterday, "{corrupt")

	got, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ServerTrafficRankingToday) != 1 || got.ServerTrafficRankingToday[0].Name != "" {
		t.Fatalf("today's servers = %+v, want the ranking without names", got.ServerTrafficRankingToday)
	}
	if got.UserTrafficRankingYesterday != nil || got.ServerTrafficRankingYesterday != nil {
		t.Fatalf("corrupt archives produced rankings: %+v / %+v", got.UserTrafficRankingYesterday, got.ServerTrafficRankingYesterday)
	}
}

func TestServerTotalDataReportsFailedQueries(t *testing.T) {
	for name, fail := range map[string]func(w *world){
		"user ranking":    func(w *world) { w.traffic.usersErr = errBackend },
		"server ranking":  func(w *world) { w.traffic.serversErr = errBackend },
		"traffic summary": func(w *world) { w.traffic.totalErr = errBackend },
		"online users":    func(w *world) { w.nodes.onlineErr = errBackend },
		"server status":   func(w *world) { w.nodes.countErr = errBackend },
		"archive": func(w *world) {
			if err := w.db.Migrator().DropTable(&log.SystemLog{}); err != nil {
				panic(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			fail(w)
			_, err := w.svc.QueryServerTotalData(context.Background())
			if xerr.CodeOf(err) != xerr.DatabaseQueryError {
				t.Fatalf("err = %v (code %d), want a database query error", err, xerr.CodeOf(err))
			}
			if w.redis.Exists(consoleServerTotalDataCacheKey) {
				t.Fatal("a failed query was cached")
			}
		})
	}
}

func TestServerTotalDataIsCachedForAMinute(t *testing.T) {
	w := newWorld(t)
	w.nodes.online = 5
	first, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ttl := w.redis.TTL(consoleServerTotalDataCacheKey); ttl != time.Minute {
		t.Fatalf("cache ttl = %v, want 1m", ttl)
	}
	w.nodes.online = 6
	cached, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil || cached.OnlineUsers != 5 || cached.UpdatedAt != first.UpdatedAt {
		t.Fatalf("second read = %+v (err %v), want the cached snapshot", cached, err)
	}
	// An unreadable snapshot is recomputed.
	if err := w.redis.Set(consoleServerTotalDataCacheKey, "{corrupt"); err != nil {
		t.Fatal(err)
	}
	fresh, err := w.svc.QueryServerTotalData(context.Background())
	if err != nil || fresh.OnlineUsers != 6 {
		t.Fatalf("read after a corrupt snapshot = %+v (err %v), want a fresh one", fresh, err)
	}
}

func TestRevenueStatistics(t *testing.T) {
	w := newWorld(t)
	w.orders.today = readmodel.OrdersTotal{AmountTotal: 30, NewOrderAmount: 20, RenewalOrderAmount: 10}
	w.orders.month = readmodel.OrdersTotal{AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100}
	w.orders.total = readmodel.OrdersTotal{AmountTotal: 3000, NewOrderAmount: 2000, RenewalOrderAmount: 1000}
	w.orders.daily = []readmodel.OrdersTotalWithDate{{Date: "2026-09-01", AmountTotal: 3, NewOrderAmount: 2, RenewalOrderAmount: 1}}
	w.orders.monthly = []readmodel.OrdersTotalWithDate{{Date: "2026-09", AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100}}

	got, err := w.svc.QueryRevenueStatistics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.RevenueStatisticsResponse{
		Today: dto.OrdersStatistics{AmountTotal: 30, NewOrderAmount: 20, RenewalOrderAmount: 10},
		Monthly: dto.OrdersStatistics{AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100, List: []dto.OrdersStatistics{
			{Date: "2026-09-01", AmountTotal: 3, NewOrderAmount: 2, RenewalOrderAmount: 1},
		}},
		All: dto.OrdersStatistics{AmountTotal: 3000, NewOrderAmount: 2000, RenewalOrderAmount: 1000, List: []dto.OrdersStatistics{
			{Date: "2026-09", AmountTotal: 300, NewOrderAmount: 200, RenewalOrderAmount: 100},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("revenue\n%+v\nwant\n%+v", got, want)
	}

	w.orders.today.AmountTotal = 31
	if cached, err := w.svc.QueryRevenueStatistics(context.Background()); err != nil || cached.Today.AmountTotal != 30 {
		t.Fatalf("second read = %+v (err %v), want the cached snapshot", cached, err)
	}
}

func TestRevenueStatisticsFailures(t *testing.T) {
	// The per-period lists are optional: without them the totals still show.
	w := newWorld(t)
	w.orders.today = readmodel.OrdersTotal{AmountTotal: 30}
	w.orders.dailyErr, w.orders.monthlyErr = errBackend, errBackend
	got, err := w.svc.QueryRevenueStatistics(context.Background())
	if err != nil || got.Today.AmountTotal != 30 || len(got.Monthly.List) != 0 || len(got.All.List) != 0 {
		t.Fatalf("revenue without lists = %+v (err %v)", got, err)
	}

	for name, fail := range map[string]func(o *fakeOrders){
		"today": func(o *fakeOrders) { o.todayErr = errBackend },
		"month": func(o *fakeOrders) { o.monthErr = errBackend },
		"total": func(o *fakeOrders) { o.totalErr = errBackend },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			fail(w.orders)
			if _, err := w.svc.QueryRevenueStatistics(context.Background()); xerr.CodeOf(err) != xerr.DatabaseQueryError {
				t.Fatalf("err = %v, want a database query error", err)
			}
			if w.redis.Exists(consoleRevenueStatisticsCacheKey) {
				t.Fatal("a failed query was cached")
			}
		})
	}
}

func TestUserStatistics(t *testing.T) {
	w := newWorld(t)
	w.users.today, w.users.month, w.users.total = 2, 20, 200
	w.users.daily = []readmodel.UserStatisticsWithDate{{Date: "2026-09-01", Register: 2, NewOrderUsers: 1, RenewalOrderUsers: 1}}
	w.users.monthly = []readmodel.UserStatisticsWithDate{{Date: "2026-09", Register: 20, NewOrderUsers: 10, RenewalOrderUsers: 5}}
	w.orders.newToday, w.orders.renewToday = 1, 1
	w.orders.newMonth, w.orders.renewMonth = 10, 5
	w.orders.newAll, w.orders.renewAll = 100, 50

	got, err := w.svc.QueryUserStatistics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.UserStatisticsResponse{
		Today: dto.UserStatistics{Register: 2, NewOrderUsers: 1, RenewalOrderUsers: 1},
		Monthly: dto.UserStatistics{Register: 20, NewOrderUsers: 10, RenewalOrderUsers: 5, List: []dto.UserStatistics{
			{Date: "2026-09-01", Register: 2, NewOrderUsers: 1, RenewalOrderUsers: 1},
		}},
		All: dto.UserStatistics{Register: 200, NewOrderUsers: 100, RenewalOrderUsers: 50, List: []dto.UserStatistics{
			{Date: "2026-09", Register: 20, NewOrderUsers: 10, RenewalOrderUsers: 5},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("users\n%+v\nwant\n%+v", got, want)
	}
	if ttl := w.redis.TTL(consoleUserStatisticsCacheKey); ttl != time.Minute {
		t.Fatalf("cache ttl = %v, want 1m", ttl)
	}
}

// Every user statistic is optional: a failing source leaves its figure at
// zero. (A failing monthly breakdown used to leak out through the named
// result and fail the whole summary, after caching it.)
func TestUserStatisticsToleratesFailingSources(t *testing.T) {
	w := newWorld(t)
	w.users.err = errBackend
	w.orders.todayUsersErr, w.orders.monthUsersErr, w.orders.allErr = errBackend, errBackend, errBackend
	w.orders.newToday = 99
	got, err := w.svc.QueryUserStatistics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, &dto.UserStatisticsResponse{}) {
		t.Fatalf("statistics with every source failing = %+v, want zeros", got)
	}
}

func TestTicketWaitReply(t *testing.T) {
	w := newWorld(t)
	w.tickets.waiting = 4
	got, err := w.svc.QueryTicketWaitReply(context.Background())
	if err != nil || got.Count != 4 {
		t.Fatalf("waiting tickets = %+v (err %v), want 4", got, err)
	}
	w.tickets.err = errBackend
	if _, err := w.svc.QueryTicketWaitReply(context.Background()); !errors.Is(err, errBackend) {
		t.Fatalf("err = %v, want the query failure", err)
	}
}

// The demo deployment shows canned figures and touches no data source.
func TestDashboardDemoMode(t *testing.T) {
	t.Setenv("PPANEL_MODE", "Demo")
	svc := NewService(Deps{})
	ctx := context.Background()
	server, err := svc.QueryServerTotalData(ctx)
	if err != nil || server.OnlineUsers != 1688 || len(server.ServerTrafficRankingToday) != 10 {
		t.Fatalf("demo server data = %+v (err %v)", server, err)
	}
	revenue, err := svc.QueryRevenueStatistics(ctx)
	if err != nil || revenue.Today.AmountTotal != 35888 || len(revenue.All.List) != 6 {
		t.Fatalf("demo revenue = %+v (err %v)", revenue, err)
	}
	users, err := svc.QueryUserStatistics(ctx)
	if err != nil || users.All.Register != 18888 || len(users.Monthly.List) != 7 {
		t.Fatalf("demo users = %+v (err %v)", users, err)
	}
	data, _ := json.Marshal(server)
	if len(data) == 0 {
		t.Fatal("demo data does not encode")
	}
}
