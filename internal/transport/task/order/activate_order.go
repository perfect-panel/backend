// Package order holds the queue handlers of billing's order tasks: the
// activation of paid orders, the deferred close of unpaid ones, the paid and
// pending order reconcilers, the order-event outbox publication and
// retention, and the daily order report. The handlers decode their payloads
// and call the billing facade, which owns every transaction.
package order

import (
	"context"
	"encoding/json"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	orderEntity "github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Order types and statuses of the billing order entity, under the names the
// task adapters have always used.
const (
	OrderTypeSubscribe    = orderEntity.TypeSubscribe
	OrderTypeRenewal      = orderEntity.TypeRenewal
	OrderTypeResetTraffic = orderEntity.TypeResetTraffic
	OrderTypeRecharge     = orderEntity.TypeRecharge
	OrderStatusPending    = orderEntity.StatusPending
	OrderStatusPaid       = orderEntity.StatusPaid
	OrderStatusClose      = orderEntity.StatusClosed
	OrderStatusFailed     = orderEntity.StatusFailed
	OrderStatusFinished   = orderEntity.StatusFinished
)

// PaidOrderActivator is billing's activation of a paid order, identified by
// its order number. Each stage of the activation records its completion, so
// a retried or replayed task skips the stages that already committed.
type PaidOrderActivator interface {
	ActivatePaidOrder(context.Context, string) error
}

// ActivateOrderHandler activates the paid order a task names. A failure goes
// back to asynq for a retry; the paid-order reconciler re-enqueues any order
// still paid after the retries.
type ActivateOrderHandler struct{ activator PaidOrderActivator }

// NewActivateOrderHandler builds the handler over the billing facade.
func NewActivateOrderHandler(activator PaidOrderActivator) *ActivateOrderHandler {
	return &ActivateOrderHandler{activator: activator}
}

func (h *ActivateOrderHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload, err := h.parsePayload(ctx, task.Payload())
	if err != nil {
		return err
	}
	return h.activator.ActivatePaidOrder(ctx, payload.OrderNo)
}

// parsePayload decodes the order number the task carries.
func (h *ActivateOrderHandler) parsePayload(ctx context.Context, payload []byte) (*taskqueue.ForthwithActivateOrderPayload, error) {
	var p taskqueue.ForthwithActivateOrderPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.WithContext(ctx).Error("[ActivateOrder] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(payload)),
		)
		return nil, err
	}
	return &p, nil
}
