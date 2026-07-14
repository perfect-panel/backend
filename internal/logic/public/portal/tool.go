package portal

import (
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func getDiscount(discounts []dto.SubscribeDiscount, inputMonths int64) float64 {
	var finalDiscount float64 = 100

	for _, discount := range discounts {
		if inputMonths >= discount.Quantity && discount.Discount < finalDiscount {
			finalDiscount = discount.Discount
		}
	}
	return finalDiscount / float64(100)
}

func calculateCoupon(amount int64, couponInfo *coupon.Coupon) int64 {
	if couponInfo.Type == 1 {
		return int64(float64(amount) * (float64(couponInfo.Discount) / float64(100)))
	} else {
		return min(couponInfo.Discount, amount)
	}
}

func ensureCouponEnabled(couponInfo *coupon.Coupon) error {
	if couponInfo.IsEnabled() {
		return nil
	}
	return errors.Wrapf(xerr.NewErrCode(xerr.CouponDisabled), "coupon disabled")
}

func calculateFee(amount int64, config *payment.Payment) int64 {
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
	return int64(fee)
}
