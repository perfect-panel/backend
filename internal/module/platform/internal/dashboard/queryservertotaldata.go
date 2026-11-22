package dashboard

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const consoleServerTotalDataCacheKey = "console:server_total_data"
const consoleServerTotalDataCacheTTL = 60 * time.Second

// rankingSize is how many users and servers the traffic rankings list.
const rankingSize = 10

// The console resolves the subscription from SID and the account from UID, so
// both mappings stay in one place to keep the two identifiers from drifting.
func userTrafficDataFromRanking(item readmodel.UserTrafficRanking) dto.UserTrafficData {
	return dto.UserTrafficData{
		SID:      item.SubscribeId,
		UID:      item.UserId,
		Upload:   item.Upload,
		Download: item.Download,
	}
}

func userTrafficDataFromRankLog(item log.UserTraffic) dto.UserTrafficData {
	return dto.UserTrafficData{
		SID:      item.SubscribeId,
		UID:      item.UserId,
		Upload:   item.Upload,
		Download: item.Download,
	}
}

// QueryServerTotalData summarizes node traffic for the admin console: today's
// live totals and rankings, yesterday's archived rankings, the month's traffic
// so far and the online counts. The summary is cached for a minute.
func (s *Service) QueryServerTotalData(ctx context.Context) (*dto.ServerTotalDataResponse, error) {
	if demoMode() {
		return mockServerTotalData(), nil
	}
	if cached, ok := readSnapshot[dto.ServerTotalDataResponse](ctx, s.deps.Cache, consoleServerTotalDataCacheKey); ok {
		return cached, nil
	}

	now := timeutil.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// AddDate, not Add(-24h): a daylight-saving change makes one day 23 or 25
	// hours long.
	yesterday := todayStart.AddDate(0, 0, -1).Format(time.DateOnly)

	live, err := s.liveTraffic(ctx, now, todayStart)
	if err != nil {
		return nil, err
	}
	usersYesterday, err := s.archivedUserRanking(ctx, yesterday)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	s.addServerNames(ctx, names, serverIDs(live.servers))
	serversToday := serverRanking(live.servers, names)
	serversYesterday, err := s.archivedServerRanking(ctx, yesterday, names)
	if err != nil {
		return nil, err
	}
	onlineUsers, err := s.deps.Nodes.OnlineUserSubscribeGlobal(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "count online users: %v", err)
	}
	onlineServers, offlineServers, err := s.deps.Nodes.CountServersByReportStatus(ctx, now.Add(-5*time.Minute))
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "count online servers: %v", err)
	}
	earlierUpload, earlierDownload, err := s.archivedMonthTraffic(ctx, now)
	if err != nil {
		return nil, err
	}

	resp := &dto.ServerTotalDataResponse{
		OnlineUsers:                   onlineUsers,
		OnlineServers:                 onlineServers,
		OfflineServers:                offlineServers,
		TodayUpload:                   live.total.Upload,
		TodayDownload:                 live.total.Download,
		MonthlyUpload:                 live.total.Upload + earlierUpload,
		MonthlyDownload:               live.total.Download + earlierDownload,
		UpdatedAt:                     now.Unix(),
		ServerTrafficRankingToday:     serversToday,
		ServerTrafficRankingYesterday: serversYesterday,
		UserTrafficRankingToday:       userRanking(live.users),
		UserTrafficRankingYesterday:   usersYesterday,
	}
	storeSnapshot(ctx, s.deps.Cache, consoleServerTotalDataCacheKey, resp, consoleServerTotalDataCacheTTL)
	return resp, nil
}

// todayTraffic is what traffic_log says about today so far.
type todayTraffic struct {
	users   []readmodel.UserTrafficRanking
	servers []readmodel.ServerTrafficRanking
	total   readmodel.TotalTraffic
}

// liveTraffic runs today's three traffic_log aggregates concurrently.
func (s *Service) liveTraffic(ctx context.Context, now, todayStart time.Time) (todayTraffic, error) {
	var (
		live                           todayTraffic
		total                          *readmodel.TotalTraffic
		usersErr, serversErr, totalErr error
		wg                             sync.WaitGroup
	)
	wg.Go(func() { live.users, usersErr = s.deps.Traffic.TopUsersTrafficByDay(ctx, now, rankingSize) })
	wg.Go(func() { live.servers, serversErr = s.deps.Traffic.TopServersTrafficByDay(ctx, now, rankingSize) })
	wg.Go(func() {
		total, totalErr = s.deps.Traffic.QueryTrafficSummary(ctx, todayStart, todayStart.Add(24*time.Hour))
	})
	wg.Wait()

	switch {
	case usersErr != nil:
		return todayTraffic{}, xerr.Wrapf(usersErr, xerr.DatabaseQueryError, "rank today's users by traffic: %v", usersErr)
	case serversErr != nil:
		return todayTraffic{}, xerr.Wrapf(serversErr, xerr.DatabaseQueryError, "rank today's servers by traffic: %v", serversErr)
	case totalErr != nil:
		return todayTraffic{}, xerr.Wrapf(totalErr, xerr.DatabaseQueryError, "sum today's traffic: %v", totalErr)
	}
	if total != nil {
		live.total = *total
	}
	return live, nil
}

func userRanking(items []readmodel.UserTrafficRanking) []dto.UserTrafficData {
	var ranking []dto.UserTrafficData
	for _, item := range items {
		ranking = append(ranking, userTrafficDataFromRanking(item))
	}
	return ranking
}

func serverIDs(items []readmodel.ServerTrafficRanking) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ServerId)
	}
	return ids
}

// serverRanking names the ranked servers; a server that no longer exists
// keeps an empty name.
func serverRanking(items []readmodel.ServerTrafficRanking, names map[int64]string) []dto.ServerTrafficData {
	var ranking []dto.ServerTrafficData
	for _, item := range items {
		ranking = append(ranking, dto.ServerTrafficData{
			ServerId: item.ServerId,
			Name:     names[item.ServerId],
			Upload:   item.Upload,
			Download: item.Download,
		})
	}
	return ranking
}

// addServerNames records the names of the servers with the given ids. A
// failed lookup only costs the names, so it is logged rather than returned.
func (s *Service) addServerNames(ctx context.Context, names map[int64]string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	servers, err := s.deps.Nodes.QueryServerList(ctx, ids)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryServerTotalData] look up server names", logger.Field("error", err.Error()))
		return
	}
	for _, server := range servers {
		names[server.Id] = server.Name
	}
}

// archivedUserRanking returns the user ranking archived for date, in rank
// order.
func (s *Service) archivedUserRanking(ctx context.Context, date string) ([]dto.UserTrafficData, error) {
	var rank log.UserTrafficRank
	if found, err := s.archivedEntry(ctx, date, log.TypeUserTrafficRank, &rank); err != nil || !found {
		return nil, err
	}
	var ranking []dto.UserTrafficData
	for _, position := range slices.Sorted(maps.Keys(rank.Rank)) {
		ranking = append(ranking, userTrafficDataFromRankLog(rank.Rank[position]))
	}
	return ranking, nil
}

// archivedServerRanking returns the server ranking archived for date, in rank
// order, looking up the names not in names yet.
func (s *Service) archivedServerRanking(ctx context.Context, date string, names map[int64]string) ([]dto.ServerTrafficData, error) {
	var rank log.ServerTrafficRank
	if found, err := s.archivedEntry(ctx, date, log.TypeServerTrafficRank, &rank); err != nil || !found {
		return nil, err
	}
	positions := slices.Sorted(maps.Keys(rank.Rank))
	var unnamed []int64
	for _, position := range positions {
		if id := rank.Rank[position].ServerId; !hasKey(names, id) {
			unnamed = append(unnamed, id)
		}
	}
	s.addServerNames(ctx, names, unnamed)

	var ranking []dto.ServerTrafficData
	for _, position := range positions {
		entry := rank.Rank[position]
		ranking = append(ranking, dto.ServerTrafficData{
			ServerId: entry.ServerId,
			Name:     names[entry.ServerId],
			Upload:   entry.Upload,
			Download: entry.Download,
		})
	}
	return ranking, nil
}

func hasKey(names map[int64]string, id int64) bool {
	_, ok := names[id]
	return ok
}

// archivedEntry decodes into entry the log of type typ archived for date and
// reports whether there was one. A corrupt archive is logged and decoded as
// far as it goes.
func (s *Service) archivedEntry(ctx context.Context, date string, typ log.Type, entry interface{ Unmarshal([]byte) error }) (bool, error) {
	stored, err := s.deps.Logs.FindFirstByDateType(ctx, date, typ.Uint8())
	if err != nil {
		return false, xerr.Wrapf(err, xerr.DatabaseQueryError, "query the type %d archive of %s: %v", typ.Uint8(), date, err)
	}
	if stored == nil {
		return false, nil
	}
	if err := entry.Unmarshal([]byte(stored.Content)); err != nil {
		logger.WithContext(ctx).Errorw("[QueryServerTotalData] decode archived ranking",
			logger.Field("error", err.Error()), logger.Field("date", date), logger.Field("type", typ.Uint8()))
	}
	return true, nil
}

// archivedMonthTraffic sums the daily traffic archived for the days of this
// month before today; a corrupt day is logged and skipped.
func (s *Service) archivedMonthTraffic(ctx context.Context, now time.Time) (upload, download int64, err error) {
	if now.Day() == 1 {
		return 0, 0, nil
	}
	dates := make([]string, 0, now.Day()-1)
	for day := 1; day < now.Day(); day++ {
		dates = append(dates, time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location()).Format(time.DateOnly))
	}
	stats, err := s.deps.Logs.FindByDatesType(ctx, dates, log.TypeTrafficStat.Uint8())
	if err != nil {
		return 0, 0, xerr.Wrapf(err, xerr.DatabaseQueryError, "query the archived daily traffic: %v", err)
	}
	for _, stored := range stats {
		var stat log.TrafficStat
		if err := stat.Unmarshal([]byte(stored.Content)); err != nil {
			logger.WithContext(ctx).Errorw("[QueryServerTotalData] decode archived daily traffic",
				logger.Field("error", err.Error()), logger.Field("date", stored.Date))
			continue
		}
		upload += stat.Upload
		download += stat.Download
	}
	return upload, download, nil
}

func mockServerTotalData() *dto.ServerTotalDataResponse {
	now := timeutil.Now()

	// Generate server traffic ranking data for today (top 10)
	serverTrafficToday := make([]dto.ServerTrafficData, 10)
	serverNames := []string{"香港-01", "美国-洛杉矶", "日本-东京", "新加坡-01", "韩国-首尔", "台湾-01", "德国-法兰克福", "英国-伦敦", "加拿大-多伦多", "澳洲-悉尼"}
	for i := 0; i < 10; i++ {
		upload := int64(500000000 + (i * 100000000) + (i%3)*200000000)    // 500MB - 1.5GB
		download := int64(2000000000 + (i * 300000000) + (i%4)*500000000) // 2GB - 8GB
		serverTrafficToday[i] = dto.ServerTrafficData{
			ServerId: int64(i + 1),
			Name:     serverNames[i],
			Upload:   upload,
			Download: download,
		}
	}

	// Generate server traffic ranking data for yesterday (top 10)
	serverTrafficYesterday := make([]dto.ServerTrafficData, 10)
	for i := 0; i < 10; i++ {
		upload := int64(480000000 + (i * 95000000) + (i%3)*180000000)
		download := int64(1900000000 + (i * 280000000) + (i%4)*450000000)
		serverTrafficYesterday[i] = dto.ServerTrafficData{
			ServerId: int64(i + 1),
			Name:     serverNames[i],
			Upload:   upload,
			Download: download,
		}
	}

	return &dto.ServerTotalDataResponse{
		OnlineUsers:                   1688,
		OnlineServers:                 8,
		OfflineServers:                2,
		TodayUpload:                   8888888888,
		TodayDownload:                 28888888888,
		MonthlyUpload:                 288888888888,
		MonthlyDownload:               888888888888,
		UpdatedAt:                     now.Unix(),
		ServerTrafficRankingToday:     serverTrafficToday,
		ServerTrafficRankingYesterday: serverTrafficYesterday,
	}
}
