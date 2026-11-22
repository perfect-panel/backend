package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterOrderLog pages the orders created.
func (s *Service) FilterOrderLog(ctx context.Context, req *dto.FilterOrderLogRequest) (*dto.FilterOrderLogResponse, error) {
	total, list, err := logPage(ctx, s.deps.Logs, "order", filterParams(log.TypeOrderCreated, req.UserId, req.FilterLogParams), func(row *log.SystemLog, content *log.OrderCreated) dto.OrderLog {
		return withRequestMetadata(&dto.OrderLog{
			Id:             row.Id,
			UserId:         row.ObjectID,
			OrderNo:        content.OrderNo,
			OrderType:      content.OrderType,
			Quantity:       content.Quantity,
			Price:          content.Price,
			Amount:         content.Amount,
			GiftAmount:     content.GiftAmount,
			Discount:       content.Discount,
			CouponDiscount: content.CouponDiscount,
			PaymentId:      content.PaymentID,
			Method:         content.Method,
			FeeAmount:      content.FeeAmount,
			SubscribeId:    content.SubscribeID,
			Source:         content.Source,
			Timestamp:      content.Timestamp,
		}, content.Metadata)
	})
	if err != nil {
		return nil, err
	}
	// The order log answers an empty list, never null.
	if list == nil {
		list = []dto.OrderLog{}
	}
	return &dto.FilterOrderLogResponse{Total: total, List: list}, nil
}
