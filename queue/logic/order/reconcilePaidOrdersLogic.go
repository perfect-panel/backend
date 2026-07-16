package orderLogic

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/queue/types"
)

const paidOrderReconcileBatchSize = 500

// ReconcilePaidOrdersLogic treats the durable Paid state as an activation
// outbox. It repairs the database/Redis gap if a callback committed payment but
// Redis was unavailable before the activation task could be inserted.
type ReconcilePaidOrdersLogic struct {
	svc *svc.ServiceContext
}

func NewReconcilePaidOrdersLogic(svc *svc.ServiceContext) *ReconcilePaidOrdersLogic {
	return &ReconcilePaidOrdersLogic{svc: svc}
}

func (l *ReconcilePaidOrdersLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	var afterID int64
	for {
		orders, err := l.svc.Store.Order().QueryOrdersByStatusAfterID(ctx, OrderStatusPaid, afterID, paidOrderReconcileBatchSize)
		if err != nil {
			return err
		}
		for _, orderInfo := range orders {
			payload, err := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderInfo.OrderNo})
			if err != nil {
				return err
			}
			task := asynq.NewTask(types.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
			_, err = l.svc.Queue.EnqueueContext(ctx, task, asynq.TaskID(types.ActivationTaskID(orderInfo.OrderNo)))
			if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
				logger.WithContext(ctx).Error("[ReconcilePaidOrders] Enqueue failed",
					logger.Field("orderNo", orderInfo.OrderNo),
					logger.Field("error", err.Error()),
				)
				return err
			}
			afterID = orderInfo.Id
		}
		if len(orders) < paidOrderReconcileBatchSize {
			return nil
		}
	}
}
