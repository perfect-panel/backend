package checkout

import (
	"context"
	"encoding/json"

	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	logEntity "github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// enqueueDeferredClose schedules the pending order's expiry close. Failures
// are logged, not fatal: the pending-order reconciler re-drives expiry.
func (s *Service) enqueueDeferredClose(ctx context.Context, tag, orderNo string) {
	if err := s.deps.Queue.EnqueueDeferredClose(ctx, orderNo); err != nil {
		logger.WithContext(ctx).Errorw(tag+" Enqueue task error", logger.Field("error", err.Error()), logger.Field("orderNo", orderNo))
	} else {
		logger.WithContext(ctx).Infow(tag+" Enqueue task success", logger.Field("orderNo", orderNo))
	}
}

// Purchase processes new subscription purchase orders including validation, discount calculation,
// coupon processing, gift amount deduction, fee calculation, and order creation with database transaction.
// It handles the complete purchase workflow from user validation to order creation and task scheduling.
func (s *Service) Purchase(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PurchaseOrderResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if req.Quantity <= 0 {
		log.Debugf("[Purchase] Quantity is less than or equal to 0, setting to 1")
		req.Quantity = 1
	}

	// Validate quantity limit
	if req.Quantity > MaxQuantity {
		log.Errorw("[Purchase] Quantity exceeds maximum limit", logger.Field("quantity", req.Quantity), logger.Field("max", MaxQuantity))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "quantity exceeds maximum limit of %d", MaxQuantity)
	}

	if s.deps.SingleModel {
		hasBlockingSubscription, err := s.deps.UserSubs.HasBlockingSubscription(ctx, u.Id)
		if err != nil {
			log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check user subscription error: %v", err.Error())
		}
		if hasBlockingSubscription {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserSubscribeExist), "user has subscription")
		}
	}

	// find subscribe plan
	sub, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}

	// check subscribe plan inventory
	if sub.Inventory == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
	}

	// check subscribe plan limit
	if sub.Quota > 0 {
		count, err := s.deps.UserSubs.CountQuotaConsumingSubscriptions(ctx, u.Id, req.SubscribeId)
		if err != nil {
			log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id), logger.Field("subscribe_id", req.SubscribeId))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "count user subscriptions error: %v", err.Error())
		}
		if count >= sub.Quota {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "quota limit")
		}
	}

	var discount float64 = 1
	if sub.Discount != "" {
		var dis []dto.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	// discount amount
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	// Validate amount to prevent overflow
	if amount > MaxOrderAmount {
		log.Errorw("[Purchase] Order amount exceeds maximum limit",
			logger.Field("amount", amount),
			logger.Field("max", MaxOrderAmount),
			logger.Field("user_id", u.Id),
			logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	var coupon int64 = 0
	// Calculate the coupon deduction
	if req.Coupon != "" {
		couponInfo, err := s.deps.Coupons.FindOneByCode(ctx, req.Coupon)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotExist), "coupon not found")
			}
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if err := ensureCouponEnabled(couponInfo); err != nil {
			return nil, err
		}
		if couponInfo.Count != 0 && couponInfo.Count <= couponInfo.UsedCount {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used")
		}
		couponSub := tool.StringToInt64Slice(couponInfo.Subscribe)
		if len(couponSub) > 0 && !tool.Contains(couponSub, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}
		count, err := s.deps.Orders.CountUserCouponUsage(ctx, u.Id, req.Coupon)
		if err != nil {
			log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id), logger.Field("coupon", req.Coupon))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if couponInfo.UserLimit > 0 && count >= couponInfo.UserLimit {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon limit exceeded")
		}
		coupon = calculateCoupon(amount, couponInfo)
	}
	// Calculate the handling fee
	amount -= coupon
	// find payment method
	payment, err := s.deps.Payments.FindOne(ctx, req.Payment)
	if err != nil {
		log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(payment); err != nil {
		return nil, err
	}
	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, payment)
		amount += feeAmount

		// Final validation after adding fee
		if amount > MaxOrderAmount {
			log.Errorw("[Purchase] Final order amount exceeds maximum limit after fee",
				logger.Field("amount", amount),
				logger.Field("max", MaxOrderAmount),
				logger.Field("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
		}
	}

	// query user is new purchase or renewal
	isNew, err := s.deps.Orders.IsUserEligibleForNewOrder(ctx, u.Id)
	if err != nil {
		log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user order error: %v", err.Error())
	}
	// create order
	orderInfo := &order.Order{
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           1,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		GiftAmount:     0,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		FeeAmount:      feeAmount,
		Status:         1,
		IsNew:          isNew,
		SubscribeId:    req.SubscribeId,
	}
	orderflow.ApplyIdempotency(ctx, orderInfo)
	// Database transaction. The per-user quota check must re-run under the
	// wallet row lock, which reads the subscription domain: this is the
	// documented transitional exception on the generic transaction (ADR-001
	// step 5 moves the serialisation into the subscription module).
	err = s.deps.Store.InTx(ctx, func(txStore repository.Store) error {
		// The request-context user is only an authentication snapshot. Lock and
		// re-read the account before reserving gift credit so two concurrent
		// orders cannot spend the same balance.
		lockedUser, e := txStore.User().FindOneForUpdate(ctx, u.Id)
		if e != nil {
			return e
		}

		if sub.Quota > 0 {
			count, e := txStore.UserSubscription().CountQuotaConsumingSubscriptions(ctx, u.Id, req.SubscribeId)
			if e != nil {
				log.Errorw("[Purchase] Database query error", logger.Field("error", e.Error()), logger.Field("user_id", u.Id), logger.Field("subscribe_id", req.SubscribeId))
				return e
			}
			if count >= sub.Quota {
				return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "quota limit")
			}
		}
		if orderInfo.Coupon != "" {
			reserved, e := txStore.Coupon().ReserveUsage(ctx, orderInfo.Coupon, timeutil.Now().Unix())
			if e != nil {
				return e
			}
			if !reserved {
				return errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used or expired")
			}
			orderInfo.CouponReserved = true
		}

		// Gift credit is reserved only after the row lock.  The fee has already
		// been calculated on the full external payable amount by design.
		if lockedUser.GiftAmount > 0 && orderInfo.Amount > 0 {
			orderInfo.GiftAmount = min(lockedUser.GiftAmount, orderInfo.Amount)
			orderInfo.Amount -= orderInfo.GiftAmount
		}
		if orderInfo.GiftAmount > 0 {
			lockedUser.GiftAmount -= orderInfo.GiftAmount
			if e := txStore.User().UpdateBalanceFields(ctx, lockedUser); e != nil {
				log.Errorw("[Purchase] Database update error", logger.Field("error", e.Error()), logger.Field("user", lockedUser))
				return e
			}
			// create deduction record
			giftLog := logEntity.Gift{
				Type:        logEntity.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     lockedUser.GiftAmount,
				Remark:      "Purchase order deduction",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if e := txStore.Log().Insert(ctx, &logEntity.SystemLog{
				Type:     logEntity.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: lockedUser.Id,
				Content:  string(content),
			}); e != nil {
				log.Errorw("[Purchase] Database insert error",
					logger.Field("error", e.Error()),
					logger.Field("deductionLog", giftLog),
				)
				return e
			}
		}

		// insert order
		return txStore.Order().Insert(ctx, orderInfo)
	})
	if err != nil {
		log.Errorw("[Purchase] Database insert error", logger.Field("error", err.Error()), logger.Field("orderInfo", orderInfo))
		var codeErr *xerr.CodeError
		if errors.As(err, &codeErr) {
			return nil, err
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert order error: %v", err.Error())
	}
	// Reserve plan inventory in its own subscription-domain transaction
	// (ADR-001 step 2). On failure the just-created order is closed, which
	// releases the coupon reservation and refunds the gift deduction; the
	// restore step no-ops because nothing was reserved.
	if err := s.reserveInventory(ctx, orderInfo.OrderNo, sub.Id); err != nil {
		if closeErr := s.Close(ctx, &dto.CloseOrderRequest{OrderNo: orderInfo.OrderNo}); closeErr != nil {
			log.Errorw("[Purchase] Close order after reservation failure failed", logger.Field("error", closeErr.Error()), logger.Field("orderNo", orderInfo.OrderNo))
		}
		if errors.Is(err, orderflow.ErrOutOfStock) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "reserve inventory error: %v", err.Error())
	}
	// Deferred task
	s.enqueueDeferredClose(ctx, "[Purchase]", orderInfo.OrderNo)

	return &dto.PurchaseOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}

// reserveInventory reserves one plan inventory unit for the order in its own
// subscription-domain transaction (idempotent via the domain event inbox).
func (s *Service) reserveInventory(ctx context.Context, orderNo string, subscribeID int64) error {
	return orderflow.ReserveInventoryOnce(ctx, s.deps.Store, orderNo, subscribeID)
}
