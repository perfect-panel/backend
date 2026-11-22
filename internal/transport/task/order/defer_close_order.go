package order

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	"github.com/perfect-panel/server/internal/module/billing"
	dto "github.com/perfect-panel/server/internal/module/billing/contract"
	"github.com/perfect-panel/server/pkg/logger"
)

// DeferCloseOrderHandler closes a pending order once its payment window has
// passed. A failed close is retried for the first three attempts only; the
// pending-order reconciler closes whatever is left afterwards.
type DeferCloseOrderHandler struct {
	deps Dependencies
}

// NewDeferCloseOrderHandler builds the handler over the billing facade.
func NewDeferCloseOrderHandler(deps Dependencies) *DeferCloseOrderHandler {
	return &DeferCloseOrderHandler{
		deps: deps,
	}
}

func (h *DeferCloseOrderHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload := taskqueue.DeferCloseOrderPayload{}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[DeferCloseOrder] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", string(task.Payload())),
		)
		return nil
	}

	err := h.deps.Billing.CloseOrder(ctx, &dto.CloseOrderRequest{
		OrderNo: payload.OrderNo,
	})
	if errors.Is(err, billing.ErrGatewayUnconfirmed) {
		// Expected for EPay orders the gateway cannot confirm as paid: the
		// order stays pending and the reconciler keeps watching it, so
		// retrying this task would only repeat the same refusal.
		logger.WithContext(ctx).Infow("[DeferCloseOrder] order stays pending until the gateway confirms payment",
			logger.Field("orderNo", payload.OrderNo),
		)
		return nil
	}
	count, ok := asynq.GetRetryCount(ctx)
	if !ok {
		return nil
	}
	if err != nil && count < 3 {
		return err
	}
	return nil
}
