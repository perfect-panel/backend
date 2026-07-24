package portal

import (
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func getDiscount(discounts []dto.SubscribeDiscount, inputMonths int64) float64 {
	var finalDiscount float64 = 100

	for _, discount := range discounts {
		if discount.Quantity > 0 && discount.Discount >= 0 && discount.Discount <= 100 && inputMonths >= discount.Quantity && discount.Discount < finalDiscount {
			finalDiscount = discount.Discount
		}
	}
	return finalDiscount / float64(100)
}

func ensurePaymentAvailable(paymentInfo *payment.Payment) error {
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

func calculateFee(amount int64, config *payment.Payment) int64 {
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
