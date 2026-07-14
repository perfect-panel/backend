package traffic

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
)

type StatLogic struct {
	svc *svc.ServiceContext
}

func NewStatLogic(svc *svc.ServiceContext) *StatLogic {
	return &StatLogic{
		svc: svc,
	}
}

func (l *StatLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	now := timeutil.Now()

	// 获取全部有效订阅
	// 获取统计时间范围
	start := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, timeutil.Location())
	end := start.Add(24 * time.Hour)

	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// 查询用户流量统计, 按用户和订阅分组
		userTraffic, err := store.TrafficLog().QueryUserTrafficRanking(ctx, start, end)
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Query user traffic failed: %v", err.Error())
			return err
		}

		date := start.Format(time.DateOnly)

		userTop10 := log.UserTrafficRank{
			Rank: make(map[uint8]log.UserTraffic),
		}

		// 更新用户流量统计
		for i, trafficData := range userTraffic {
			item := log.UserTraffic{
				SubscribeId: trafficData.SubscribeId,
				UserId:      trafficData.UserId,
				Upload:      trafficData.Upload,
				Download:    trafficData.Download,
				Total:       trafficData.Total,
			}
			if i < 10 {
				userTop10.Rank[uint8(i+1)] = item
			}
			// 更新用户流量统计日志
			content, _ := item.Marshal()
			err = store.Log().Insert(ctx, &log.SystemLog{
				Type:     log.TypeSubscribeTraffic.Uint8(),
				Date:     date,
				ObjectID: item.SubscribeId,
				Content:  string(content),
			})
			if err != nil {
				logger.Errorf("[Traffic Stat Queue] Create user traffic log failed: %v", err.Error())
				return err
			}
		}

		userTop10Content, _ := userTop10.Marshal()

		// 更新用户排行榜
		err = store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeUserTrafficRank.Uint8(),
			Date:     date,
			ObjectID: 0, // 0表示全局用户排行榜
			Content:  string(userTop10Content),
		})
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Create user traffic rank log failed: %v", err.Error())
			return err
		}

		// 统计服务器流量
		serverTraffic, err := store.TrafficLog().QueryServerTrafficRanking(ctx, start, end)
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Query server traffic failed: %v", err.Error())
			return err
		}

		serverTop10 := log.ServerTrafficRank{
			Rank: make(map[uint8]log.ServerTraffic),
		}
		for i, trafficData := range serverTraffic {
			item := log.ServerTraffic{
				ServerId: trafficData.ServerId,
				Upload:   trafficData.Upload,
				Download: trafficData.Download,
				Total:    trafficData.Total,
			}
			if i < 10 {
				serverTop10.Rank[uint8(i+1)] = item
			}
			// 更新服务器流量统计日志
			content, _ := item.Marshal()
			err = store.Log().Insert(ctx, &log.SystemLog{
				Type:     log.TypeServerTraffic.Uint8(),
				Date:     date,
				ObjectID: item.ServerId,
				Content:  string(content),
			})
			if err != nil {
				logger.Errorf("[Traffic Stat Queue] Create server traffic log failed: %v", err.Error())
				return err
			}
		}
		serverTop10Content, _ := serverTop10.Marshal()
		// 更新服务器排行榜
		err = store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeServerTrafficRank.Uint8(),
			Date:     date,
			ObjectID: 0, // 0表示全局服务器排行榜
			Content:  string(serverTop10Content),
		})
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Create server traffic rank log failed: %v", err.Error())
			return err
		}

		// traffic stat
		summary, err := store.TrafficLog().QueryTrafficSummary(ctx, start, end)
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Query traffic stat failed: %v", err.Error())
			return err
		}
		stat := log.TrafficStat{
			Upload:   summary.Upload,
			Download: summary.Download,
			Total:    summary.Upload + summary.Download,
		}

		// 更新流量统计日志
		content, _ := stat.Marshal()
		err = store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeTrafficStat.Uint8(),
			Date:     date,
			ObjectID: 0,
			Content:  string(content),
		})
		if err != nil {
			logger.Errorf("[Traffic Stat Queue] Create traffic stat log failed: %v", err.Error())
			return err
		}

		// Delete old traffic logs
		if l.svc.Config.Log.AutoClear {
			threshold := end.AddDate(0, 0, int(-l.svc.Config.Log.ClearDays))
			err = store.TrafficLog().DeleteBefore(ctx, threshold)
			if err != nil {
				logger.Errorf("[Traffic Stat Queue] Delete server traffic log failed: %v", err.Error())
			}
			// Delete old system logs
			if err = store.Log().DeleteBefore(ctx, threshold); err != nil {
				logger.Errorf("[Traffic Stat Queue] Delete old system logs failed: %v", err.Error())
			}
		}
		return nil
	})
	if err != nil {
		logger.Errorf("[Traffic Stat Queue] Process task failed: %v", err.Error())
		return err
	}
	logger.Infof("[Traffic Stat Queue] Process task completed successfully, consuming: %s", time.Since(now).String())
	return nil
}
