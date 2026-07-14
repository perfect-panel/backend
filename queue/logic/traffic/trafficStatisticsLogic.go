package traffic

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
)

//goland:noinspection GoNameStartsWithPackageName
type TrafficStatisticsLogic struct {
	svc *svc.ServiceContext
}

func NewTrafficStatisticsLogic(svc *svc.ServiceContext) *TrafficStatisticsLogic {
	return &TrafficStatisticsLogic{
		svc: svc,
	}
}

func (l *TrafficStatisticsLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload types.TrafficStatistics
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[TrafficStatistics] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(task.Payload())),
		)
		return nil
	}
	if len(payload.Logs) == 0 {
		logger.WithContext(ctx).Error("[TrafficStatistics] Payload is empty")
		return nil
	}
	// query server info
	serverInfo, err := l.svc.Store.Node().FindOneServer(ctx, payload.ServerId)
	if err != nil {
		logger.WithContext(ctx).Error("[TrafficStatistics] Find server info failed",
			logger.Field("serverId", payload.ServerId),
			logger.Field("error", err.Error()),
		)
		return nil
	}
	// query protocol ratio
	// default ratio is 1.0

	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		logger.Errorf("[TrafficStatistics] Unmarshal protocols failed: %s", err.Error())
		return nil
	}
	var protocol *node.Protocol

	var ratio float32 = 1.0

	for _, p := range protocols {
		if strings.ToLower(p.Type) == strings.ToLower(payload.Protocol) {
			protocol = &p
			break
		}
	}

	if protocol == nil {
		logger.WithContext(ctx).Error("[TrafficStatistics] Protocol not found: %s", payload.Protocol)
		return nil
	}

	// use protocol ratio if it's greater than 0
	if protocol.Ratio > 0 {
		ratio = float32(protocol.Ratio)
	}

	now := timeutil.Now()
	realTimeMultiplier := l.svc.NodeMultiplierManager.GetMultiplier(now)
	logger.Debugf("[TrafficStatisticsLogic] Current time traffic multiplier: %.2f", realTimeMultiplier)
	for _, log := range payload.Logs {
		// query user Subscribe Info
		sub, err := l.svc.Store.User().FindOneSubscribe(ctx, log.SID)
		if err != nil {
			logger.WithContext(ctx).Error("[TrafficStatistics] Find user Subscribe Info failed",
				logger.Field("uid", log.SID),
				logger.Field("error", err.Error()),
			)
			continue
		}

		if log.Download+log.Upload <= l.svc.Config.Node.TrafficReportThreshold {
			// no traffic, skip
			continue
		}
		// update user subscribe with log
		d := int64(float32(log.Download) * ratio * realTimeMultiplier)
		u := int64(float32(log.Upload) * ratio * realTimeMultiplier)
		if err := l.svc.Store.User().UpdateUserSubscribeWithTraffic(ctx, sub.Id, d, u); err != nil {
			logger.WithContext(ctx).Error("[TrafficStatistics] Update user subscribe with log failed",
				logger.Field("sid", log.SID),
				logger.Field("download", float32(log.Download)*ratio),
				logger.Field("upload", float32(log.Upload)*ratio),
				logger.Field("error", err.Error()),
			)
			continue
		}

		// create log log
		if err = l.svc.Store.TrafficLog().Insert(ctx, &traffic.TrafficLog{
			ServerId:    payload.ServerId,
			SubscribeId: log.SID,
			UserId:      sub.UserId,
			Upload:      u,
			Download:    d,
			Timestamp:   now,
		}); err != nil {
			logger.WithContext(ctx).Error("[TrafficStatistics] Create log log failed",
				logger.Field("uid", log.SID),
				logger.Field("download", float32(log.Download)*ratio),
				logger.Field("upload", float32(log.Upload)*ratio),
				logger.Field("error", err.Error()),
			)
		}
	}
	return nil
}
