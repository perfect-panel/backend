package orderLogic

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

const (
	pendingOrderReconcileBatchSize = 500
	pendingOrderExpiry             = 15 * time.Minute
)

// ReconcilePendingOrdersLogic is a durable fallback for deferred close tasks.
// State transitions in CloseOrder remain conditional, so a late callback can
// safely race this scan without turning a paid order back into a close order.
type ReconcilePendingOrdersLogic struct {
	svc *svc.ServiceContext
}

func NewReconcilePendingOrdersLogic(svc *svc.ServiceContext) *ReconcilePendingOrdersLogic {
	return &ReconcilePendingOrdersLogic{svc: svc}
}

func (l *ReconcilePendingOrdersLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	var afterID int64
	cutoff := time.Now().Add(-pendingOrderExpiry)
	for {
		orders, err := l.svc.Store.Order().QueryOrdersByStatusAfterID(ctx, OrderStatusPending, afterID, pendingOrderReconcileBatchSize)
		if err != nil {
			return err
		}
		for _, orderInfo := range orders {
			afterID = orderInfo.Id
			if orderInfo.CreatedAt.After(cutoff) {
				continue
			}
			if err := l.svc.Billing.CloseOrder(ctx, &dto.CloseOrderRequest{OrderNo: orderInfo.OrderNo}); err != nil {
				// Keep the order pending for a later reconciliation instead of
				// failing the entire batch because one gateway is unavailable.
				logger.WithContext(ctx).Error("[ReconcilePendingOrders] close failed",
					logger.Field("orderNo", orderInfo.OrderNo),
					logger.Field("error", err.Error()),
				)
			}
		}
		if len(orders) < pendingOrderReconcileBatchSize {
			return nil
		}
	}
}
