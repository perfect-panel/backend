package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterBalanceLog pages a user's balance movements.
func (s *Service) FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error) {
	params := filterParams(log.TypeBalance, req.UserId, req.FilterLogParams)
	// The balance log is not searched.
	params.Search = ""
	total, list, err := logPage(ctx, s.deps.Logs, "balance", params, func(row *log.SystemLog, content *log.Balance) dto.BalanceLog {
		return withRequestMetadata(&dto.BalanceLog{
			UserId:    row.ObjectID,
			Amount:    content.Amount,
			Type:      content.Type,
			OrderNo:   content.OrderNo,
			Balance:   content.Balance,
			Timestamp: content.Timestamp,
		}, content.Metadata)
	})
	if err != nil {
		return nil, err
	}
	// The balance log answers an empty list, never null.
	if list == nil {
		list = []dto.BalanceLog{}
	}
	return &dto.FilterBalanceLogResponse{Total: total, List: list}, nil
}
