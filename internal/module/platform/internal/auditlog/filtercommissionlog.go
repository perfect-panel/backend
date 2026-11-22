package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterCommissionLog pages a user's commission movements.
func (s *Service) FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error) {
	params := filterParams(log.TypeCommission, req.UserId, req.FilterLogParams)
	// The commission log is not searched.
	params.Search = ""
	total, list, err := logPage(ctx, s.deps.Logs, "commission", params, func(row *log.SystemLog, content *log.Commission) dto.CommissionLog {
		return withRequestMetadata(&dto.CommissionLog{
			UserId:    row.ObjectID,
			Type:      content.Type,
			Amount:    content.Amount,
			OrderNo:   content.OrderNo,
			Timestamp: content.Timestamp,
		}, content.Metadata)
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterCommissionLogResponse{Total: total, List: list}, nil
}
