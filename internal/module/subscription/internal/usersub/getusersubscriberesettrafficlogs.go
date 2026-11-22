package usersub

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribeResetTrafficLogs pages a subscription's traffic reset log.
func (s *Service) GetUserSubscribeResetTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeResetTrafficLogsRequest) (*dto.GetUserSubscribeResetTrafficLogsResponse, error) {
	lg := logger.WithContext(ctx)
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeResetSubscribe.Uint8(),
		ObjectID: req.UserSubscribeId,
	})
	if err != nil {
		lg.Errorf("[ResetSubscribeTrafficLog] failed to filter system log: %v", err)
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FilterSystemLog failed, err: %v", err)
	}

	var list []dto.ResetSubscribeTrafficLog

	for _, item := range data {
		var content log.ResetSubscribe
		if err = content.Unmarshal([]byte(item.Content)); err != nil {
			lg.Errorf("[ResetSubscribeTrafficLog] failed to unmarshal log: %v", err)
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt reset subscription log %d: %v", item.Id, err)
		}
		list = append(list, dto.ResetSubscribeTrafficLog{
			Id:               item.Id,
			Type:             content.Type,
			OrderNo:          content.OrderNo,
			Timestamp:        content.Timestamp,
			UserSubscribeId:  item.ObjectID,
			ClientIP:         content.ClientIP,
			UserAgent:        content.UserAgent,
			ActorID:          content.ActorID,
			IPCountryCode:    content.IPCountryCode,
			IPCountry:        content.IPCountry,
			IPRegion:         content.IPRegion,
			IPCity:           content.IPCity,
			IPASN:            content.IPASN,
			IPASOrganization: content.IPASOrganization,
		})
	}

	return &dto.GetUserSubscribeResetTrafficLogsResponse{
		Total: total,
		List:  list,
	}, nil
}
