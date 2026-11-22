package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribeTrafficLogs pages a subscription's traffic records.
func (s *Service) GetUserSubscribeTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeTrafficLogsRequest) (*dto.GetUserSubscribeTrafficLogsResponse, error) {
	list, total, err := s.deps.Traffic.SubscriptionTrafficLogs(ctx, req.UserId, req.SubscribeId, req.Page, req.Size)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list the traffic logs of subscription %d", req.SubscribeId)
	}
	logs := make([]dto.TrafficLog, 0, len(list))
	for _, record := range list {
		logs = append(logs, dto.TrafficLog{
			Id:          record.Id,
			ServerId:    record.ServerId,
			UserId:      record.UserId,
			SubscribeId: record.SubscribeId,
			Download:    record.Download,
			Upload:      record.Upload,
			Timestamp:   record.Timestamp.UnixMilli(),
		})
	}
	return &dto.GetUserSubscribeTrafficLogsResponse{
		Total: total,
		List:  logs,
	}, nil
}
