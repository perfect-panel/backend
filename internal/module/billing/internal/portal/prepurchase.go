package portal

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// PrePurchase calculates the guest order pricing preview without creating an
// order.
func (s *Service) PrePurchase(ctx context.Context, req *dto.PrePurchaseOrderRequest) (*dto.PrePurchaseOrderResponse, error) {
	log := logger.WithContext(ctx)
	// find subscribe plan
	sub, err := s.deps.Plans.FindOne(ctx, req.SubscribeId)
	if err != nil {
		log.Errorw("[PreCreateOrder] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
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
	var coupon int64
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
		subs := tool.StringToInt64Slice(couponInfo.Subscribe)

		if len(subs) > 0 && !tool.Contains(subs, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}

		coupon = calculateCoupon(amount, couponInfo)
	}
	amount -= coupon
	var feeAmount int64
	if req.Payment != 0 {
		payment, err := s.deps.Payments.FindOne(ctx, req.Payment)
		if err != nil {
			log.Errorw("[PreCreateOrder] Database query error", logger.Field("error", err.Error()), logger.Field("payment", req.Payment))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
		}
		if err := ensurePaymentAvailable(payment); err != nil {
			return nil, err
		}
		// Calculate the handling fee
		if amount > 0 {
			feeAmount = calculateFee(amount, payment)
		}
		amount += feeAmount
	}

	return &dto.PrePurchaseOrderResponse{
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		FeeAmount:      feeAmount,
	}, nil
}
