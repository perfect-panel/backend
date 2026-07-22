package notify

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/stripe"
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
	if notify.EventType != "payment_intent.succeeded" {
		return nil
	}
	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
	if err != nil {
		l.Logger.Error("[StripeNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
	}
	if finished, err := validateStripeCallback(l.ctx, orderInfo, stripeConfig, &config, notify); err != nil {
		return err
	} else if finished {
		return nil
	}
	paid, err := client.QueryOrderStatus(notify.TradeNo)
	if err != nil {
		return err
	}
	if !paid {
		return errors.New("Stripe payment intent is not paid")
	}
	if err := markOrderPaidAndEnqueue(l.ctx, l.svcCtx, orderInfo, notify.TradeNo); err != nil {
		return err
	}
	l.Infow("[StripeNotify] success", logger.Field("orderNo", notify.OrderNo))
	return nil
}

func validateStripeCallback(ctx context.Context, orderInfo *order.Order, paymentConfig *payment.Payment, config *payment.StripeConfig, notify *stripe.NotifyResult) (bool, error) {
	if notify == nil {
		return false, errors.New("Stripe callback is missing")
	}
	if err := validateOrderPayment(orderInfo, paymentConfig); err != nil {
		return false, err
	}
	if notify.Method == "" || notify.Method != config.Payment {
		return false, errors.New("Stripe payment method mismatch")
	}
	if finished, err := finishedOrderDuplicate(ctx, orderInfo, notify.TradeNo); err != nil || finished {
		return finished, err
	}
	if err := validateOrderCanSettle(orderInfo); err != nil {
		return false, err
	}
	if err := validatePaymentExpectation(orderInfo, notify.Amount, strings.ToUpper(notify.Currency)); err != nil {
		return false, err
	}
	return false, nil
}
