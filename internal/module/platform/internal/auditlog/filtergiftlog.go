package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterGiftLog pages a user's gift-amount movements.
func (s *Service) FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error) {
	total, list, err := logPage(ctx, s.deps.Logs, "gift", filterParams(log.TypeGift, req.UserId, req.FilterLogParams), func(row *log.SystemLog, content *log.Gift) dto.GiftLog {
		return withRequestMetadata(&dto.GiftLog{
			Type:        content.Type,
			UserId:      row.ObjectID,
			OrderNo:     content.OrderNo,
			SubscribeId: content.SubscribeId,
			Amount:      content.Amount,
			Balance:     content.Balance,
			Remark:      content.Remark,
			Timestamp:   content.Timestamp,
		}, content.Metadata)
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterGiftLogResponse{Total: total, List: list}, nil
}
