package order

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/alipay"
)

type CloseOrderLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCloseOrderLogic Close order
func NewCloseOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CloseOrderLogic {
	return &CloseOrderLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CloseOrderLogic) CloseOrder(req *dto.CloseOrderRequest) error {
	store := l.svcCtx.Store
	// Find order information by order number
	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		l.Errorw("[CloseOrder] Find order info failed",
			logger.Field("error", err.Error()),
			logger.Field("orderNo", req.OrderNo),
		)
		return nil
	}
	// If the order status is not 1, it means that the order has been closed or paid
	if orderInfo.Status != 1 {
		l.Infow("[CloseOrder] Order status is not 1",
			logger.Field("orderNo", req.OrderNo),
			logger.Field("status", orderInfo.Status),
		)
		return nil
	}
	settled, err := l.settleOrCancelStripeOrder(orderInfo)
	if err != nil {
		return err
	}
	if settled {
		return nil
	}

	// Only query subscribe info if SubscribeId is valid
	var sub *subscribe.Subscribe
	if orderInfo.SubscribeId > 0 {
		sub, err = store.Subscribe().FindOne(l.ctx, orderInfo.SubscribeId)
		if err != nil {
			l.Errorw("[CloseOrder] Find subscribe info failed",
				logger.Field("error", err.Error()),
				logger.Field("subscribeId", orderInfo.SubscribeId),
			)
			return nil
		}
	}

	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		// Only the still-pending order may be closed.  A payment callback can
		// race this task, so an unconditional status write would otherwise turn
		// a paid order back into a closed order.
		closed, err := txStore.Order().UpdateOrderStatusFrom(l.ctx, req.OrderNo, 1, 3)
		if err != nil {
			l.Errorw("[CloseOrder] Update order status failed",
				logger.Field("error", err.Error()),
				logger.Field("orderNo", req.OrderNo),
			)
			return err
		}
		if !closed {
			return nil
		}
		// Keep closed guest orders for payment audit and reconciliation.  Deleting
		// them used to discard evidence of a late provider payment and, because
		// of the early return, also skipped restoration of reserved inventory.
		// refund deduction amount to user deduction balance
		if orderInfo.GiftAmount > 0 {
			userInfo, err := txStore.User().FindOne(l.ctx, orderInfo.UserId)
			if err != nil {
				l.Errorw("[CloseOrder] Find user info failed",
					logger.Field("error", err.Error()),
					logger.Field("user_id", orderInfo.UserId),
				)
				return err
			}
			deduction := userInfo.GiftAmount + orderInfo.GiftAmount
			userInfo.GiftAmount = deduction
			err = txStore.User().Update(l.ctx, userInfo)
			if err != nil {
				l.Errorw("[CloseOrder] Refund deduction amount failed",
					logger.Field("error", err.Error()),
					logger.Field("uid", orderInfo.UserId),
					logger.Field("deduction", orderInfo.GiftAmount),
				)
				return err
			}
			// Record the deduction refund log

			giftLog := log.Gift{
				Type:        log.GiftTypeIncrease,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     deduction,
				Remark:      "Order cancellation refund",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			err = txStore.Log().Insert(l.ctx, &log.SystemLog{
				Id:       0,
				Type:     log.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: userInfo.Id,
				Content:  string(content),
			})
			if err != nil {
				l.Errorw("[CloseOrder] Record cancellation refund log failed",
					logger.Field("error", err.Error()),
					logger.Field("uid", orderInfo.UserId),
					logger.Field("deduction", orderInfo.GiftAmount),
				)
				return err
			}
		}
		// Restore subscribe inventory if subscribe exists
		if sub != nil {
			if sub.Inventory != -1 {
				sub.Inventory++
				if e := txStore.Subscribe().Update(l.ctx, sub); e != nil {
					l.Errorw("[CloseOrder] Restore subscribe inventory failed",
						logger.Field("error", e.Error()),
						logger.Field("subscribeId", sub.Id),
					)
					return e
				}
			}
		}

		return nil
	})
	if err != nil {
		logger.Errorf("[CloseOrder] Transaction failed: %v", err.Error())
		return err
	}
	return nil
}

// settleOrCancelStripeOrder closes the provider-side authorization before the
// local order is closed.  Without this, a user can complete an old Stripe
// client secret after the 15-minute local expiry and be charged for an order
// the callback is no longer allowed to settle.
func (l *CloseOrderLogic) settleOrCancelStripeOrder(orderInfo *order.Order) (bool, error) {
	if paymentPlatform.ParsePlatform(orderInfo.Method) != paymentPlatform.Stripe || orderInfo.TradeNo == "" {
		return false, nil
	}
	paymentConfig, err := l.svcCtx.Store.Payment().FindOne(l.ctx, orderInfo.PaymentId)
	if err != nil {
		return false, err
	}
	config := payment.StripeConfig{}
	if err := json.Unmarshal([]byte(paymentConfig.Config), &config); err != nil {
		return false, err
	}
	client := stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
	})
	stripeOrder := &stripe.Order{
		OrderNo:   orderInfo.OrderNo,
		Subscribe: "", // subscribe metadata is informational; immutable payment fields below are authoritative.
		Amount:    orderInfo.Amount,
		Currency:  l.svcCtx.Config.Currency.Unit,
		Payment:   config.Payment,
	}
	paid, err := client.VerifyPaymentIntent(stripeOrder, orderInfo.TradeNo)
	if err != nil {
		return false, err
	}
	if paid {
		if err := notify.SettleVerifiedPayment(l.ctx, l.svcCtx, orderInfo, orderInfo.TradeNo); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := client.CancelPaymentIntent(orderInfo.TradeNo); err == nil {
		return false, nil
	}

	// A payment can finish between the status query and cancellation.  Recheck
	// once so that case is settled rather than closed locally.
	paid, err = client.VerifyPaymentIntent(stripeOrder, orderInfo.TradeNo)
	if err != nil {
		return false, err
	}
	if !paid {
		return false, fmt.Errorf("cancel Stripe payment intent %s failed", orderInfo.TradeNo)
	}
	if err := notify.SettleVerifiedPayment(l.ctx, l.svcCtx, orderInfo, orderInfo.TradeNo); err != nil {
		return false, err
	}
	return true, nil
}

// confirmationPayment Determine whether the payment is successful
//
//nolint:unused
func (l *CloseOrderLogic) confirmationPayment(order *order.Order) bool {
	paymentConfig, err := l.svcCtx.Store.Payment().FindOne(l.ctx, order.PaymentId)
	if err != nil {
		l.Errorw("[CloseOrder] Find payment config failed", logger.Field("error", err.Error()), logger.Field("paymentMark", order.Method))
		return false
	}
	switch order.Method {
	case AlipayF2f:
		if l.queryAlipay(paymentConfig, order.TradeNo) {
			return true
		}
	case StripeAlipay:
		if l.queryStripe(paymentConfig, order.TradeNo) {
			return true
		}
	case StripeWeChatPay:
		if l.queryStripe(paymentConfig, order.TradeNo) {
			return true
		}
	default:
		l.Infow("[CloseOrder] Unsupported payment method", logger.Field("paymentMethod", order.Method))
	}
	return false
}

// queryAlipay Query Alipay payment status
//
//nolint:unused
func (l *CloseOrderLogic) queryAlipay(paymentConfig *payment.Payment, TradeNo string) bool {
	config := payment.AlipayF2FConfig{}
	if err := json.Unmarshal([]byte(paymentConfig.Config), &config); err != nil {
		l.Errorw("[CloseOrder] Unmarshal payment config failed", logger.Field("error", err.Error()), logger.Field("paymentId", paymentConfig.Id))
		return false
	}
	client := alipay.NewClient(alipay.Config{
		AppId:       config.AppId,
		PrivateKey:  config.PrivateKey,
		PublicKey:   config.PublicKey,
		InvoiceName: config.InvoiceName,
		Sandbox:     config.Sandbox,
	})
	if client == nil {
		return false
	}
	status, err := client.QueryTrade(l.ctx, TradeNo)
	if err != nil {
		l.Errorw("[CloseOrder] Query trade failed", logger.Field("error", err.Error()), logger.Field("TradeNo", TradeNo))
		return false
	}
	if status == alipay.Success || status == alipay.Finished {
		return true
	}
	return false
}

// queryStripe Query Stripe payment status
//
//nolint:unused
func (l *CloseOrderLogic) queryStripe(paymentConfig *payment.Payment, TradeNo string) bool {
	config := payment.StripeConfig{}
	if err := json.Unmarshal([]byte(paymentConfig.Config), &config); err != nil {
		l.Errorw("[CloseOrder] Unmarshal payment config failed", logger.Field("error", err.Error()), logger.Field("paymentId", paymentConfig.Id))
		return false
	}
	client := stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
	})
	status, err := client.QueryOrderStatus(TradeNo)
	if err != nil {
		l.Errorw("[CloseOrder] Query order status failed", logger.Field("error", err.Error()), logger.Field("TradeNo", TradeNo))
		return false
	}
	return status
}
