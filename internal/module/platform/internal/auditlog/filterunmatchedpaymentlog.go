package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterUnmatchedPaymentLog pages the payments a gateway confirmed that
// could not settle their order (the order was closed or finished, the trade
// differs from the bound one, or the gateway asks for a review): what an
// operator refunds from. user_id narrows to one payer; search matches the
// order and trade numbers and the reason.
func (s *Service) FilterUnmatchedPaymentLog(ctx context.Context, req *dto.FilterUnmatchedPaymentLogRequest) (*dto.FilterUnmatchedPaymentLogResponse, error) {
	total, list, err := logList[log.UnmatchedPayment](ctx, s.deps.Logs, "unmatched payment", log.TypeUnmatchedPayment, req.UserId, req.FilterLogParams, unmatchedPaymentView)
	if err != nil {
		return nil, err
	}
	return &dto.FilterUnmatchedPaymentLogResponse{Total: total, List: list}, nil
}

// unmatchedPaymentView shows one unmatched payment with the request that
// reported it.
func unmatchedPaymentView(row *log.SystemLog, content *log.UnmatchedPayment) dto.UnmatchedPaymentLog {
	view := &dto.UnmatchedPaymentLog{Id: row.Id, UserId: row.ObjectID, Timestamp: content.Timestamp, CreatedAt: row.CreatedAt.UnixMilli()}
	view.OrderNo, view.TradeNo, view.Platform = content.OrderNo, content.TradeNo, content.Platform
	view.Amount, view.Currency, view.Reason = content.Amount, content.Currency, content.Reason
	return withRequestMetadata(view, content.Metadata)
}
