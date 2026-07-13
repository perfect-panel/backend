package notify

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/queue/types"
)

type StripeNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewStripeNotifyLogic Stripe notify
func NewStripeNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StripeNotifyLogic {
	return &StripeNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StripeNotifyLogic) StripeNotify(payload []byte, signature string) error {
	store := l.svcCtx.Store
	stripeConfig, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}
	config := payment.StripeConfig{}
	if err := json.Unmarshal([]byte(stripeConfig.Config), &config); err != nil {
		return err
	}
	client := stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
	})

	notify, err := client.ParseNotify(payload, signature)
	if err != nil {
		l.Errorw("[StripeNotify] error", logger.Field("errors", err.Error()))
		return err
	}
	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
	if err != nil {
		l.Logger.Error("[StripeNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
	}
	if notify.EventType == "payment_intent.succeeded" {
		if orderInfo.Status == 5 {
			return nil
		}
		// update order status and trade_no
		err = store.Order().UpdateOrderStatusAndTradeNo(l.ctx, notify.OrderNo, 2, notify.TradeNo)
		if err != nil {
			return err
		}
		// create ActivateOrder task
		payload := types.ForthwithActivateOrderPayload{
			OrderNo: notify.OrderNo,
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			l.Errorw("[StripeNotify] Marshal error", logger.Field("errors", err.Error()), logger.Field("payload", payload))
			return err
		}
		task := asynq.NewTask(types.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
		_, err = l.svcCtx.Queue.Enqueue(task)
		if err != nil {
			l.Errorw("[StripeNotify] Enqueue error", logger.Field("errors", err.Error()))
			return err
		}
		l.Infow("[StripeNotify] success", logger.Field("orderNo", notify.OrderNo))
	}
	return nil
}
