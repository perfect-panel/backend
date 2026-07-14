package order

import (
	"github.com/perfect-panel/server/internal/model/entity/coupon"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func ensureCouponEnabled(couponInfo *coupon.Coupon) error {
	if couponInfo.IsEnabled() {
		return nil
	}
	return errors.Wrapf(xerr.NewErrCode(xerr.CouponDisabled), "coupon disabled")
}

func calculateCoupon(amount int64, couponInfo *coupon.Coupon) int64 {
	if couponInfo.Type == 1 {
		return int64(float64(amount) * (float64(couponInfo.Discount) / float64(100)))
	} else {
		return min(couponInfo.Discount, amount)
	}
}
