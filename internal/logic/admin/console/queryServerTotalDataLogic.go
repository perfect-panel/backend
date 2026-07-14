package console

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

const consoleServerTotalDataCacheKey = "console:server_total_data"
const consoleServerTotalDataCacheTTL = 60 * time.Second

type QueryServerTotalDataLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryServerTotalDataLogic Query server total data
func NewQueryServerTotalDataLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryServerTotalDataLogic {
	return &QueryServerTotalDataLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryServerTotalDataLogic) QueryServerTotalData() (resp *dto.ServerTotalDataResponse, err error) {

	if strings.ToLower(os.Getenv("PPANEL_MODE")) == "demo" {
		return l.mockRevenueStatistics(), nil
	}

	// Try cache first
	cached, cacheErr := l.svcCtx.Redis.Get(l.ctx, consoleServerTotalDataCacheKey).Result()
	if cacheErr == nil && cached != "" {
		var result dto.ServerTotalDataResponse
		if json.Unmarshal([]byte(cached), &result) == nil {
			return &result, nil
		}
	}

	now := timeutil.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.Add(24 * time.Hour)
	trafficStore := l.svcCtx.Store.TrafficLog()
	logStore := l.svcCtx.Store.Log()
	nodeStore := l.svcCtx.Store.Node()

	// Parallelize three traffic_log queries to reduce latency
	var (
		todayTop10User                 []traffic.UserTrafficRanking
		todayTop10Server               []traffic.ServerTrafficRanking
		todayTraffic                   *traffic.TotalTraffic
		userErr, serverErr, trafficErr error
		wg                             sync.WaitGroup
	)

	wg.Add(3)

	// Query 1: Today's top 10 users by traffic
	go func() {
		defer wg.Done()
		todayTop10User, userErr = trafficStore.TopUsersTrafficByDay(l.ctx, now, 10)
	}()

	// Query 2: Today's top 10 servers by traffic
	go func() {
		defer wg.Done()
		todayTop10Server, serverErr = trafficStore.TopServersTrafficByDay(l.ctx, now, 10)
	}()

	// Query 3: Today's total upload/download
	go func() {
		defer wg.Done()
		todayTraffic, trafficErr = trafficStore.QueryTrafficSummary(l.ctx, todayStart, todayEnd)
	}()

	wg.Wait()

	if userErr != nil {
		logger.Errorf("[QueryServerTotalData] Query user traffic failed: %v", userErr.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query user traffic failed: %v", userErr.Error())
	}
	if serverErr != nil {
		logger.Errorf("[QueryServerTotalData] Query server traffic failed: %v", serverErr.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query server traffic failed: %v", serverErr.Error())
	}
	if trafficErr != nil {
		logger.Errorf("[QueryServerTotalData] Sum today traffic failed: %v", trafficErr.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Sum today traffic failed: %v", trafficErr.Error())
	}

	// Build today user traffic ranking
	var userTodayTrafficRanking []dto.UserTrafficData
	for _, item := range todayTop10User {
		userTodayTrafficRanking = append(userTodayTrafficRanking, dto.UserTrafficData{
			SID:      item.SubscribeId,
			Upload:   item.Upload,
			Download: item.Download,
		})
	}

	// Query yesterday user traffic rank log
	yesterday := todayStart.Add(-24 * time.Hour).Format(time.DateOnly)

	yesterdayLog, err := logStore.FindFirstByDateType(l.ctx, yesterday, log.TypeUserTrafficRank.Uint8())
	if err != nil {
		l.Errorw("[QueryServerTotalDataLogic] Query yesterday user traffic rank log error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query yesterday user traffic rank log error: %v", err)
	}

	var yesterdayUserRankData []dto.UserTrafficData
	if yesterdayLog != nil {
		var rank log.UserTrafficRank
		err = rank.Unmarshal([]byte(yesterdayLog.Content))
		if err != nil {
			l.Errorw("[QueryServerTotalDataLogic] Unmarshal yesterday user traffic rank log error", logger.Field("error", err.Error()))
		}
		// Extract and sort keys to maintain rank order
		keys := make([]uint8, 0, len(rank.Rank))
		for k := range rank.Rank {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, k := range keys {
			yesterdayUserRankData = append(yesterdayUserRankData, dto.UserTrafficData{
				SID:      rank.Rank[k].SubscribeId,
				Upload:   rank.Rank[k].Upload,
				Download: rank.Rank[k].Download,
			})
		}
	}

	// Batch fetch server names for today's ranking
	serverIDs := make([]int64, 0, 10)
	for _, item := range todayTop10Server {
		serverIDs = append(serverIDs, item.ServerId)
	}

	serverMap := make(map[int64]string)
	if len(serverIDs) > 0 {
		servers, err := nodeStore.QueryServerList(l.ctx, serverIDs)
		if err != nil {
			l.Errorw("[QueryServerTotalDataLogic] Batch fetch servers error", logger.Field("error", err.Error()))
		}
		for _, s := range servers {
			serverMap[s.Id] = s.Name
		}
	}

	var todayServerRanking []dto.ServerTrafficData
	for _, item := range todayTop10Server {
		name := ""
		if serverName, ok := serverMap[item.ServerId]; ok {
			name = serverName
		}
		todayServerRanking = append(todayServerRanking, dto.ServerTrafficData{
			ServerId: item.ServerId,
			Name:     name,
			Upload:   item.Upload,
			Download: item.Download,
		})
	}

	// Query yesterday server traffic rank
	var yesterdayTop10Server []dto.ServerTrafficData
	yesterdayServerTrafficLog, err := logStore.FindFirstByDateType(l.ctx, yesterday, log.TypeServerTrafficRank.Uint8())
	if err != nil {
		l.Errorw("[QueryServerTotalDataLogic] Query yesterday server traffic rank log error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query yesterday server traffic rank log error: %v", err)
	}
	if yesterdayServerTrafficLog != nil {
		var rank log.ServerTrafficRank
		err = rank.Unmarshal([]byte(yesterdayServerTrafficLog.Content))
		if err != nil {
			l.Errorw("[QueryServerTotalDataLogic] Unmarshal yesterday server traffic rank log error", logger.Field("error", err.Error()))
		}

		// Extract and sort keys to maintain rank order
		serverKeys := make([]uint8, 0, len(rank.Rank))
		for k := range rank.Rank {
			serverKeys = append(serverKeys, k)
		}
		sort.Slice(serverKeys, func(i, j int) bool { return serverKeys[i] < serverKeys[j] })

		// Collect yesterday server IDs not already fetched
		yesterdayServerIDs := make([]int64, 0, len(rank.Rank))
		for _, k := range serverKeys {
			v := rank.Rank[k]
			if _, ok := serverMap[v.ServerId]; !ok {
				yesterdayServerIDs = append(yesterdayServerIDs, v.ServerId)
			}
		}
		if len(yesterdayServerIDs) > 0 {
			if extraServers, err := nodeStore.QueryServerList(l.ctx, yesterdayServerIDs); err == nil {
				for _, s := range extraServers {
					serverMap[s.Id] = s.Name
				}
			}
		}

		for _, k := range serverKeys {
			v := rank.Rank[k]
			name := ""
			if serverName, ok := serverMap[v.ServerId]; ok {
				name = serverName
			}
			yesterdayTop10Server = append(yesterdayTop10Server, dto.ServerTrafficData{
				ServerId: v.ServerId,
				Name:     name,
				Upload:   v.Upload,
				Download: v.Download,
			})
		}
	}

	// Query online user count
	onlineUsers, err := l.svcCtx.Store.Node().OnlineUserSubscribeGlobal(l.ctx)
	if err != nil {
		l.Errorw("[QueryServerTotalDataLogic] OnlineUserSubscribeGlobal error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "OnlineUserSubscribeGlobal error: %v", err)
	}

	// Query online/offline server count
	onlineServers, offlineServers, err := nodeStore.CountServersByReportStatus(l.ctx, now.Add(-5*time.Minute))
	if err != nil {
		l.Errorw("[QueryServerTotalDataLogic] Count online servers error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Count online servers error: %v", err)
	}

	// Monthly traffic: today's real-time data + archived daily stats for previous days
	todayUpload := todayTraffic.Upload
	todayDownload := todayTraffic.Download
	var monthlyUpload, monthlyDownload int64
	monthlyUpload += todayUpload
	monthlyDownload += todayDownload

	// Batch query all previous days' traffic stats (eliminates N+1 loop)
	if now.Day() > 1 {
		dates := make([]string, 0, now.Day()-1)
		for i := 1; i < int(now.Day()); i++ {
			d := time.Date(now.Year(), now.Month(), i, 0, 0, 0, 0, now.Location()).Format(time.DateOnly)
			dates = append(dates, d)
		}

		dailyLogs, err := logStore.FindByDatesType(l.ctx, dates, log.TypeTrafficStat.Uint8())
		if err != nil {
			l.Errorw("[QueryServerTotalDataLogic] Batch query daily traffic stats error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Batch query daily traffic stats error: %v", err)
		}

		for _, logInfo := range dailyLogs {
			var stat log.TrafficStat
			if err := stat.Unmarshal([]byte(logInfo.Content)); err != nil {
				l.Errorw("[QueryServerTotalDataLogic] Unmarshal daily traffic stat error", logger.Field("error", err.Error()), logger.Field("date", logInfo.Date))
				continue
			}
			monthlyUpload += stat.Upload
			monthlyDownload += stat.Download
		}
	}

	resp = &dto.ServerTotalDataResponse{
		OnlineUsers:                   onlineUsers,
		OnlineServers:                 onlineServers,
		OfflineServers:                offlineServers,
		TodayUpload:                   todayUpload,
		TodayDownload:                 todayDownload,
		MonthlyUpload:                 monthlyUpload,
		MonthlyDownload:               monthlyDownload,
		UpdatedAt:                     now.Unix(),
		ServerTrafficRankingToday:     todayServerRanking,
		ServerTrafficRankingYesterday: yesterdayTop10Server,
		UserTrafficRankingToday:       userTodayTrafficRanking,
		UserTrafficRankingYesterday:   yesterdayUserRankData,
	}

	// Cache the result
	if data, marshalErr := json.Marshal(resp); marshalErr == nil {
		l.svcCtx.Redis.Set(l.ctx, consoleServerTotalDataCacheKey, data, consoleServerTotalDataCacheTTL)
	}

	return resp, nil
}

func (l *QueryServerTotalDataLogic) mockRevenueStatistics() *dto.ServerTotalDataResponse {
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
