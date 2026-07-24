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

// Renewal processes subscription renewal orders including discount calculation,
// coupon validation, gift amount deduction, fee calculation, and order creation
func (s *Service) Renewal(ctx context.Context, req *dto.RenewalOrderRequest) (*dto.RenewalOrderResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if req.Quantity <= 0 {
		log.Debugf("[Renewal] Quantity is less than or equal to 0, setting to 1")
		req.Quantity = 1
	}

	// Validate quantity limit
	if req.Quantity > MaxQuantity {
		log.Errorw("[Renewal] Quantity exceeds maximum limit", logger.Field("quantity", req.Quantity), logger.Field("max", MaxQuantity))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "quantity exceeds maximum limit of %d", MaxQuantity)
	}

	orderNo := tool.GenerateTradeNo()
	// find user subscribe
	userSubscribe, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, req.UserSubscribeID)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user subscribe error: %v", err.Error())
	}
	if userSubscribe.UserId != u.Id {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "subscription does not belong to the current user")
	}
	if userSubscribe.Status == user.SubscribeStatusDeducted {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "deducted subscription cannot be renewed")
	}
	// find subscription
	sub, err := s.deps.Plans.FindOne(ctx, userSubscribe.SubscribeId)
	if err != nil {
		log.Errorw("[Renewal] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", userSubscribe.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}
	var discount float64 = 1
	if sub.Discount != "" {
		var dis []dto.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	// Validate amount to prevent overflow
	if amount > MaxOrderAmount {
		log.Errorw("[Renewal] Order amount exceeds maximum limit",
			logger.Field("amount", amount),
			logger.Field("max", MaxOrderAmount),
			logger.Field("user_id", u.Id),
			logger.Field("subscribe_id", sub.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	var coupon int64 = 0
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
		if len(couponSub) > 0 && !tool.Contains(couponSub, sub.Id) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}
		count, err := s.deps.Orders.CountUserCouponUsage(ctx, u.Id, req.Coupon)
		if err != nil {
			log.Errorw("[Renewal] Database query error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id), logger.Field("coupon", req.Coupon))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if couponInfo.UserLimit > 0 && count >= couponInfo.UserLimit {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon limit exceeded")
		}
		coupon = calculateCoupon(amount, couponInfo)
	}
	payment, err := s.deps.Payments.FindOne(ctx, req.Payment)
	if err != nil {
		log.Errorw("[Renewal] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(payment); err != nil {
		return nil, err
	}
	amount -= coupon

	// create order
	orderInfo := order.Order{
		UserId:         u.Id,
		ParentId:       userSubscribe.OrderId,
		OrderNo:        orderNo,
		Type:           2,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		GiftAmount:     0,
		Discount:       discountAmount,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		FeeAmount:      0,
		Status:         1,
		SubscribeId:    userSubscribe.SubscribeId,
		SubscribeToken: userSubscribe.Token,
	}
	orderflow.ApplyIdempotency(ctx, &orderInfo)
	// Billing-domain transaction: wallet deduction, coupon reservation and
	// order creation settle together.
	err = s.deps.Store.InBillingTx(ctx, func(txStore repository.BillingStore) error {
		lockedUser, e := txStore.Wallet().FindOneForUpdate(ctx, u.Id)
		if e != nil {
			return e
		}
		if lockedUser.GiftAmount > 0 && orderInfo.Amount > 0 {
			orderInfo.GiftAmount = min(lockedUser.GiftAmount, orderInfo.Amount)
			orderInfo.Amount -= orderInfo.GiftAmount
		}
		if orderInfo.Amount > 0 {
			orderInfo.FeeAmount = calculateFee(orderInfo.Amount, payment)
			orderInfo.Amount += orderInfo.FeeAmount
		}
		if orderInfo.Amount > MaxOrderAmount {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
		}
		if orderInfo.Coupon != "" {
			reserved, reserveErr := txStore.Coupon().ReserveUsage(ctx, orderInfo.Coupon, timeutil.Now().Unix())
			if reserveErr != nil {
				return reserveErr
			}
			if !reserved {
				return errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used or expired")
			}
			orderInfo.CouponReserved = true
		}

		if orderInfo.GiftAmount > 0 {
			lockedUser.GiftAmount -= orderInfo.GiftAmount
			if err := txStore.Wallet().UpdateBalanceFields(ctx, lockedUser); err != nil {
				log.Errorw("[Renewal] Database update error", logger.Field("error", err.Error()), logger.Field("user", lockedUser))
				return err
			}
			// create deduction record
			giftLog := logEntity.Gift{
				Type:        logEntity.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     lockedUser.GiftAmount,
				Remark:      "Renewal order deduction",
				Timestamp:   timeutil.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if err := txStore.Log().Insert(ctx, &logEntity.SystemLog{
				Type:     logEntity.TypeGift.Uint8(),
				Date:     timeutil.Now().Format(time.DateOnly),
				ObjectID: lockedUser.Id,
				Content:  string(content),
			}); err != nil {
				log.Errorw("[Renewal] Database insert error", logger.Field("error", err.Error()), logger.Field("deductionLog", giftLog))
				return err
			}
		}
		// insert order
		return txStore.Order().Insert(ctx, &orderInfo)
	})
	if err != nil {
		log.Errorw("[Renewal] Database insert error", logger.Field("error", err.Error()), logger.Field("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	s.enqueueDeferredClose(ctx, "[Renewal]", orderInfo.OrderNo)
	return &dto.RenewalOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
