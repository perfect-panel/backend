package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterResetSubscribeLog pages a subscription's traffic resets.
func (s *Service) FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error) {
	total, list, err := logPage(ctx, s.deps.Logs, "reset subscription", filterParams(log.TypeResetSubscribe, req.UserSubscribeId, req.FilterLogParams), func(row *log.SystemLog, content *log.ResetSubscribe) dto.ResetSubscribeLog {
		return withRequestMetadata(&dto.ResetSubscribeLog{
			Type:            content.Type,
			UserId:          content.UserId,
			UserSubscribeId: row.ObjectID,
			OrderNo:         content.OrderNo,
			Timestamp:       content.Timestamp,
		}, content.Metadata)
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterResetSubscribeLogResponse{Total: total, List: list}, nil
}
