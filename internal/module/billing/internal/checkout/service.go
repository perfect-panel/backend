// Package checkout implements the user-facing money flows of the billing
// module: purchase, renewal, traffic reset, recharge, order preview and
// close. Only the module facade may reach it.
package checkout

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/coupon"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	paymentEntity "github.com/perfect-panel/server/internal/model/entity/payment"
	subscribeEntity "github.com/perfect-panel/server/internal/model/entity/subscribe"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Order lifecycle constants shared with the V2 orchestration layer via the
// module facade.
const (
	CloseOrderTimeMinutes = 15

	// MaxOrderAmount Order amount limits
	MaxOrderAmount    = 2147483647 // int32 max value (2.1 billion)
	MaxRechargeAmount = 2000000000 // 2 billion, slightly lower for safety
	MinRechargeAmount = 100        // minimum recharge amount in minor currency units
	MaxQuantity       = 1000       // Maximum quantity per order
)

// PlanReader is the module's port onto the subscription domain's plan
// catalogue; the legacy subscribe repository satisfies it structurally.
type PlanReader interface {
	FindOne(ctx context.Context, id int64) (*subscribeEntity.Subscribe, error)
}

// UserSubscriptionReader is the module's port onto the subscription domain's
// user subscriptions; the legacy user-subscription repository satisfies it
// structurally.
type UserSubscriptionReader interface {
	HasBlockingSubscription(ctx context.Context, userID int64) (bool, error)
	CountQuotaConsumingSubscriptions(ctx context.Context, userID, subscribeID int64) (int64, error)
	FindOneUserSubscribe(ctx context.Context, id int64) (*userEntity.SubscribeDetails, error)
	FindOneSubscribe(ctx context.Context, id int64) (*userEntity.Subscribe, error)
}

// OrderQueue mirrors the facade's order queue port.
type OrderQueue interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
	EnqueueDeferredClose(ctx context.Context, orderNo string) error
}

type Deps struct {
	Orders   repository.OrderRepo
	Coupons  repository.CouponRepo
	Payments repository.PaymentRepo
	Plans    PlanReader
	UserSubs UserSubscriptionReader
	// Store is the transitional full-store dependency: the purchase
	// transaction re-checks the subscription quota under the wallet row lock
	// (ADR-001 step 5 moves that concern), and the inventory lifecycle
	// helpers need the store's scoped transactions and inbox.
	Store repository.Store
	Queue OrderQueue
	// SingleModel forbids holding more than one blocking subscription.
	SingleModel bool
	// CurrencyUnit is the site currency used for gateway verification.
	CurrencyUnit string
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func getDiscount(discounts []dto.SubscribeDiscount, inputMonths int64) float64 {
	var finalDiscount float64 = 100

	for _, discount := range discounts {
		if discount.Quantity > 0 && discount.Discount >= 0 && discount.Discount <= 100 && inputMonths >= discount.Quantity && discount.Discount < finalDiscount {
			finalDiscount = discount.Discount
		}
	}

	return finalDiscount / float64(100)
}

func ensureCouponEnabled(couponInfo *coupon.Coupon) error {
	if !couponInfo.IsEnabled() {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponDisabled), "coupon disabled")
	}
	now := timeutil.Now().Unix()
	if couponInfo.StartTime > 0 && now < couponInfo.StartTime {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon is not active")
	}
	if couponInfo.ExpireTime <= 0 || now > couponInfo.ExpireTime {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponExpired), "coupon expired")
	}
	return nil
}

func ensurePaymentAvailable(paymentInfo *paymentEntity.Payment) error {
	if paymentInfo == nil || paymentInfo.Enable == nil || !*paymentInfo.Enable || paymentPlatform.ParsePlatform(paymentInfo.Platform) == paymentPlatform.UNSUPPORTED {
		return errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "payment method is unavailable")
	}
	return nil
}

func calculateCoupon(amount int64, couponInfo *coupon.Coupon) int64 {
	if amount <= 0 || couponInfo == nil || couponInfo.Discount < 0 {
		return 0
	}
	if couponInfo.Type == 1 {
		if couponInfo.Discount > 100 {
			return amount
		}
		return int64(float64(amount) * (float64(couponInfo.Discount) / float64(100)))
	}
	return min(couponInfo.Discount, amount)
}

func calculateFee(amount int64, config *paymentEntity.Payment) int64 {
	if amount <= 0 || config == nil || config.FeePercent < 0 || config.FeeAmount < 0 {
		return 0
	}
	var fee float64
	switch config.FeeMode {
	case 0:
		return 0
	case 1:
		fee = float64(amount) * (float64(config.FeePercent) / float64(100))
	case 2:
		if amount > 0 {
			fee = float64(config.FeeAmount)
		}
	case 3:
		fee = float64(amount)*(float64(config.FeePercent)/float64(100)) + float64(config.FeeAmount)
	}
	if fee < 0 {
		return 0
	}
	return int64(fee)
}

func validateTradeNo(tradeNo string) error {
	if tradeNo == "" || len(tradeNo) > 255 || strings.TrimSpace(tradeNo) != tradeNo || !utf8.ValidString(tradeNo) {
		return errors.New("invalid trade number")
	}
	for _, char := range tradeNo {
		if char < 0x20 || char == 0x7f {
			return errors.New("invalid trade number")
		}
	}
	return nil
}

// settleVerifiedPayment marks a gateway-verified payment as paid and enqueues
// activation. Callers must authenticate the gateway response and verify the
// order amount before invoking it. The committed Paid state is the durable
// outbox: an enqueue failure is repaired by paid-order reconciliation.
func (s *Service) settleVerifiedPayment(ctx context.Context, orderInfo *orderEntity.Order, tradeNo string) error {
	if err := validateTradeNo(tradeNo); err != nil {
		return err
	}
	if orderInfo.TradeNo != "" && orderInfo.TradeNo != tradeNo {
		return errors.New("order trade number mismatch")
	}

	switch orderInfo.Status {
	case 5: // finished
		return nil
	case 2: // paid
		// A prior callback may have committed the database update but failed to
		// contact Redis. Re-enqueue below so retries heal that partial failure.
	case 1: // pending
		updated, err := s.deps.Orders.MarkOrderPaid(ctx, orderInfo.OrderNo, tradeNo)
		if err != nil {
			return err
		}
		if !updated {
			latest, err := s.deps.Orders.FindOneByOrderNo(ctx, orderInfo.OrderNo)
			if err != nil {
				return err
			}
			if latest.TradeNo != "" && latest.TradeNo != tradeNo {
				return errors.New("order trade number mismatch")
			}
			if latest.Status == 5 {
				return nil
			}
			if latest.Status != 2 {
				return errors.Errorf("invalid order status transition: %d", latest.Status)
			}
		}
	default:
		return errors.Errorf("invalid order status transition: %d", orderInfo.Status)
	}

	return s.deps.Queue.EnqueueActivation(ctx, orderInfo.OrderNo)
}
