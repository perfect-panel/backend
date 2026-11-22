package serverapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/pkg/timeutil"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/internal/trafficagg"
	"github.com/perfect-panel/backend/pkg/logger"
)

// ServerPushUserTraffic adds a server's traffic report to the aggregation
// pipeline, which bills it to the subscriptions in batches.
func (s *Service) ServerPushUserTraffic(ctx context.Context, req *dto.ServerPushUserTrafficRequest) error {
	log := logger.WithContext(ctx)
	serverInfo, err := s.deps.Servers.FindOneServer(ctx, req.ServerId)
	if err != nil {
		log.Errorw("[ServerPushUserTraffic] FindOne error", logger.Field("error", err))
		return errors.New("server not found")
	}

	aggregator := trafficagg.New(trafficagg.Deps{
		Usage:         s.deps.TrafficUsage,
		Subscriptions: s.deps.Subscriptions,
		Reports:       s.deps.Reports,
		Redis:         s.deps.Redis,
		Multiplier:    s.deps.Multiplier,
		// A node may only bill the subscriptions its user list hands it.
		ServedSubscriptions: s.servedSubscriptionIDs,
	})
	logs := dtoTrafficToAggregator(req.Traffic)
	if req.ReportID != "" {
		err = aggregator.AdmitIdentified(ctx, serverInfo, req.Protocol, req.ReportID, logs, timeutil.Now())
	} else {
		err = aggregator.AddReport(ctx, serverInfo, req.Protocol, logs)
	}
	if err != nil {
		log.Errorw("[ServerPushUserTraffic] Aggregate traffic error", logger.Field("error", err.Error()))
		return fmt.Errorf("aggregate traffic: %w", err)
	}
	return nil
}

// dtoTrafficToAggregator converts the reported entries into the pipeline's
// form.
func dtoTrafficToAggregator(items []dto.UserTraffic) []trafficagg.UserTraffic {
	if len(items) == 0 {
		return nil
	}
	result := make([]trafficagg.UserTraffic, 0, len(items))
	for _, item := range items {
		result = append(result, trafficagg.UserTraffic{
			SID:      item.SID,
			Upload:   item.Upload,
			Download: item.Download,
		})
	}
	return result
}
