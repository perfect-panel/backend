package portal

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// Purchase creates a guest pre-order: the billing transaction reserves the
// coupon and creates the pending order, then plan inventory is reserved in
// its own subscription-domain transaction (ADR-001 step 2).
func (s *Service) Purchase(ctx context.Context, req *dto.PortalPurchaseRequest) (*dto.PortalPurchaseResponse, error) {
	log := logger.WithContext(ctx)
	// find user auth
	userAuth, err := s.deps.UserAuths.FindUserAuthMethodByOpenID(ctx, req.AuthType, req.Identifier)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth error: %v", err.Error())
	}
	if userAuth.UserId != 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "user already exists")
	}
	// find subscribe plan
	sub, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	// check subscribe plan stock
	if sub.Inventory == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
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
	// discount amount
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	var couponAmount int64 = 0
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

		couponAmount = calculateCoupon(amount, couponInfo)
	}
	// Calculate the handling fee
	amount -= couponAmount
	// find payment method
	paymentConfig, err := s.deps.Payments.FindOne(ctx, req.Payment)
	if err != nil {
		log.Errorw("[Purchase] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "find payment method error: %v", err.Error())
	}
	if err := ensurePaymentAvailable(paymentConfig); err != nil {
		return nil, err
	}

	if payment.ParsePlatform(paymentConfig.Platform) == payment.Balance {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "balance error")
	}

	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, paymentConfig)
	}
	amount += feeAmount
	// create order
	checkoutToken := orderflow.GuestCheckoutToken(ctx)
	if checkoutToken == "" {
		checkoutToken = random.KeyNew(32, 1)
	}
	orderInfo := &order.Order{
		OrderNo:                tool.GenerateTradeNo(),
		Type:                   1,
		Quantity:               req.Quantity,
		Price:                  price,
		Amount:                 amount,
		Discount:               discountAmount,
		GiftAmount:             0,
		Coupon:                 req.Coupon,
		CouponDiscount:         couponAmount,
		PaymentId:              req.Payment,
		Method:                 paymentConfig.Platform,
		FeeAmount:              feeAmount,
		Status:                 1,
		IsNew:                  true,
		SubscribeId:            req.SubscribeId,
		GuestAuthType:          req.AuthType,
		GuestIdentifier:        req.Identifier,
		GuestPasswordHash:      tool.EncodePassWord(req.Password),
		GuestInviteCode:        req.InviteCode,
		GuestCheckoutTokenHash: constant.CheckoutTokenHash(checkoutToken),
	}
	orderflow.ApplyIdempotency(ctx, orderInfo)
	// Billing-domain transaction: coupon reservation and order creation
	// settle together.
	err = s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		if orderInfo.Coupon != "" {
			reserved, reserveErr := store.Coupon().ReserveUsage(ctx, orderInfo.Coupon, timeutil.Now().Unix())
			if reserveErr != nil {
				return reserveErr
			}
			if !reserved {
				return errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used or expired")
			}
			orderInfo.CouponReserved = true
		}

		// save guest order
		if err = store.Order().Insert(ctx, orderInfo); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Errorw("[Purchase] Database transaction error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "transaction error: %v", err.Error())
	}
	// Reserve plan inventory in its own subscription-domain transaction
	// (ADR-001 step 2). On failure the guest order is closed inline; guest
	// pre-orders only hold a coupon reservation.
	if err := orderflow.ReserveInventoryOnce(ctx, s.deps.Store, orderInfo.OrderNo, sub.Id); err != nil {
		closeErr := s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
			closed, e := store.Order().UpdateOrderStatusFrom(ctx, orderInfo.OrderNo, 1, 3)
			if e != nil {
				return e
			}
			if closed && orderInfo.CouponReserved {
				return store.Coupon().ReleaseUsage(ctx, orderInfo.Coupon)
			}
			return nil
		})
		if closeErr != nil {
			log.Errorw("[Purchase] Close order after reservation failure failed", logger.Field("error", closeErr.Error()), logger.Field("orderNo", orderInfo.OrderNo))
		}
		if errors.Is(err, orderflow.ErrOutOfStock) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "reserve inventory error: %v", err.Error())
	}
	// Deferred task
	if err := s.deps.Queue.EnqueueDeferredClose(ctx, orderInfo.OrderNo); err != nil {
		log.Errorw("[CloseOrder Task] Enqueue task error", logger.Field("error", err.Error()), logger.Field("orderNo", orderInfo.OrderNo))
	} else {
		log.Infow("[CloseOrder Task] Enqueue task success", logger.Field("orderNo", orderInfo.OrderNo))
	}
	return &dto.PortalPurchaseResponse{OrderNo: orderInfo.OrderNo, CheckoutToken: checkoutToken}, nil
}
