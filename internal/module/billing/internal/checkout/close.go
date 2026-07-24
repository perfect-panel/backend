package checkout

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/pkg/errors"
)

const orderTypeSubscribe uint8 = 1

// Close closes a pending order: the billing transaction releases the coupon
// reservation and refunds the gift deduction, then the reserved plan
// inventory returns in its own subscription-domain transaction (ADR-001
// step 2). Orders whose gateway checkout already collected money are settled
// instead of closed.
func (s *Service) Close(ctx context.Context, req *dto.CloseOrderRequest) error {
	log := logger.WithContext(ctx)
	// Find order information by order number
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, req.OrderNo)
	if err != nil {
		log.Errorw("[CloseOrder] Find order info failed",
			logger.Field("error", err.Error()),
			logger.Field("orderNo", req.OrderNo),
		)
		return nil
	}
	// Public callers are authenticated by the route. Queue workers use a
	// context without a user and are the only internal callers allowed to close
	// any expired order.
	if currentUser, ok := ctx.Value(constant.CtxKeyUser).(*user.User); ok && currentUser != nil && orderInfo.UserId != currentUser.Id {
		return errors.New("order does not belong to the current user")
	}
	// If the order status is not 1, it means that the order has been closed or paid
	if orderInfo.Status != 1 {
		log.Infow("[CloseOrder] Order status is not 1",
			logger.Field("orderNo", req.OrderNo),
			logger.Field("status", orderInfo.Status),
		)
		if orderInfo.Status == 3 {
			// Resume a restoration lost between the close commit and the
			// inventory transaction; RestoreInventoryOnce no-ops when the
			// order never reserved or already restored.
			return s.restoreReservedInventory(ctx, orderInfo)
		}
		return nil
	}
	settled, err := s.settleOrCancelGatewayOrder(ctx, orderInfo)
	if err != nil {
		return err
	}
	if settled {
		return nil
	}

	var closed bool
	err = s.deps.Store.InBillingTx(ctx, func(txStore repository.BillingStore) error {
		// Only the still-pending order may be closed.  A payment callback can
		// race this task, so an unconditional status write would otherwise turn
		// a paid order back into a closed order.
		closed, err = txStore.Order().UpdateOrderStatusFrom(ctx, req.OrderNo, 1, 3)
		if err != nil {
			log.Errorw("[CloseOrder] Update order status failed",
				logger.Field("error", err.Error()),
				logger.Field("orderNo", req.OrderNo),
			)
			return err
		}
		if !closed {
			return nil
		}
		if orderInfo.Coupon != "" && orderInfo.CouponReserved {
			if err := txStore.Coupon().ReleaseUsage(ctx, orderInfo.Coupon); err != nil {
				return err
			}
		}
		// Keep closed guest orders for payment audit and reconciliation.  Deleting
		// them used to discard evidence of a late provider payment and, because
		// of the early return, also skipped restoration of reserved inventory.
		// refund deduction amount to user deduction balance
		if orderInfo.GiftAmount > 0 {
			userInfo, err := txStore.Wallet().FindOneForUpdate(ctx, orderInfo.UserId)
			if err != nil {
				log.Errorw("[CloseOrder] Find user info failed",
					logger.Field("error", err.Error()),
					logger.Field("user_id", orderInfo.UserId),
				)
				return err
			}
			deduction := userInfo.GiftAmount + orderInfo.GiftAmount
			userInfo.GiftAmount = deduction
			err = txStore.Wallet().UpdateBalanceFields(ctx, userInfo)
			if err != nil {
				log.Errorw("[CloseOrder] Refund deduction amount failed",
					logger.Field("error", err.Error()),
					logger.Field("uid", orderInfo.UserId),
					logger.Field("deduction", orderInfo.GiftAmount),
				)
				return err
			}
			// Record the deduction refund log
			giftLog := logEntity.Gift{
				Type:        logEntity.GiftTypeIncrease,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     deduction,
				Remark:      "Order cancellation refund",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			err = txStore.Log().Insert(ctx, &logEntity.SystemLog{
				Id:       0,
				Type:     logEntity.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: userInfo.Id,
				Content:  string(content),
			})
			if err != nil {
				log.Errorw("[CloseOrder] Record cancellation refund log failed",
					logger.Field("error", err.Error()),
					logger.Field("uid", orderInfo.UserId),
					logger.Field("deduction", orderInfo.GiftAmount),
				)
				return err
			}
		}
		return nil
	})
	if err != nil {
		logger.Errorf("[CloseOrder] Transaction failed: %v", err.Error())
		return err
	}
	if !closed {
		return nil
	}
	// The reserved plan inventory returns in its own subscription-domain
	// transaction (ADR-001 step 2). A crash before this point is resumed by
	// the retried close task via the status==3 branch above.
	return s.restoreReservedInventory(ctx, orderInfo)
}

// restoreReservedInventory returns the closed order's reserved inventory
// unit. Only new subscription purchases reserve plan inventory; renewals and
// traffic resets reference a plan too, but never consumed stock, and the
// reserve marker check inside RestoreInventoryOnce keeps them (and stock-out
// compensation closes) from adding stock that was never taken.
func (s *Service) restoreReservedInventory(ctx context.Context, orderInfo *order.Order) error {
	if orderInfo.Type != orderTypeSubscribe || orderInfo.SubscribeId <= 0 {
		return nil
	}
	if err := orderflow.RestoreInventoryOnce(ctx, s.deps.Store, orderInfo.OrderNo, orderInfo.SubscribeId); err != nil {
		logger.WithContext(ctx).Errorw("[CloseOrder] Restore subscribe inventory failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribeId", orderInfo.SubscribeId),
			logger.Field("orderNo", orderInfo.OrderNo),
		)
		return err
	}
	return nil
}

// settleOrCancelGatewayOrder ensures that closing locally cannot leave an
// active provider checkout able to charge the user after stock and coupons
// have been released.
func (s *Service) settleOrCancelGatewayOrder(ctx context.Context, orderInfo *order.Order) (bool, error) {
	switch paymentPlatform.ParsePlatform(orderInfo.Method) {
	case paymentPlatform.Stripe:
		return s.settleOrCancelStripeOrder(ctx, orderInfo)
	case paymentPlatform.EPay:
		return s.settleEPayOrder(ctx, orderInfo)
	default:
		return false, nil
	}
}

func (s *Service) settleOrCancelStripeOrder(ctx context.Context, orderInfo *order.Order) (bool, error) {
	if orderInfo.TradeNo == "" {
		return false, nil
	}
	paymentConfig, err := s.deps.Payments.FindOne(ctx, orderInfo.PaymentId)
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
		Currency:  s.deps.CurrencyUnit,
		Payment:   config.Payment,
	}
	paid, err := client.VerifyPaymentIntent(stripeOrder, orderInfo.TradeNo)
	if err != nil {
		return false, err
	}
	if paid {
		if err := s.settleVerifiedPayment(ctx, orderInfo, orderInfo.TradeNo); err != nil {
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
	if err := s.settleVerifiedPayment(ctx, orderInfo, orderInfo.TradeNo); err != nil {
		return false, err
	}
	return true, nil
}

// EPay-compatible gateways have no standard cancellation API. Once a payment
// URL has been issued, retaining the pending reservation is safer than closing
// locally and accepting a later customer charge with no fulfillment. Gateways
// with an order-query endpoint are reconciled here; unsupported or unavailable
// gateways remain pending for retry/manual resolution instead of losing funds.
func (s *Service) settleEPayOrder(ctx context.Context, orderInfo *order.Order) (bool, error) {
	if orderInfo.PaymentCurrency == "" {
		return false, nil // checkout was never started; safe to close.
	}
	paymentConfig, err := s.deps.Payments.FindOne(ctx, orderInfo.PaymentId)
	if err != nil {
		return false, err
	}
	config := payment.EPayConfig{}
	if err := json.Unmarshal([]byte(paymentConfig.Config), &config); err != nil {
		return false, err
	}
	result, err := epay.NewClient(config.Pid, config.Url, config.Key, config.Type).QueryOrder(orderInfo.OrderNo)
	if err != nil {
		return false, fmt.Errorf("cannot safely expire EPay order %s: %w", orderInfo.OrderNo, err)
	}
	if !result.Paid {
		return false, fmt.Errorf("cannot safely expire unpaid EPay order %s; gateway does not provide cancellation", orderInfo.OrderNo)
	}
	if result.StatusOnly {
		return false, fmt.Errorf("cannot safely reconcile paid EPay order %s: gateway query has no transaction details", orderInfo.OrderNo)
	}
	amount, err := epay.ParseMoney(result.Money)
	if err != nil || result.OrderNo != orderInfo.OrderNo || result.MerchantID != config.Pid || result.Type != config.Type || amount != orderInfo.PaymentAmount || result.TradeNo == "" {
		return false, fmt.Errorf("EPay order %s query does not match payment expectation", orderInfo.OrderNo)
	}
	if err := s.settleVerifiedPayment(ctx, orderInfo, result.TradeNo); err != nil {
		return false, err
	}
	return true, nil
}
