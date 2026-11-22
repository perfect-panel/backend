package pricing

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// CouponFinder loads a coupon by its code.
type CouponFinder interface {
	FindOneByCode(ctx context.Context, code string) (*coupon.Coupon, error)
}

// ResolveCoupon loads the coupon an order names and checks that it may
// discount an order for planID at now. An empty code resolves to no coupon.
// The per-user limit is the caller's check: it needs the buyer's orders.
func ResolveCoupon(ctx context.Context, coupons CouponFinder, code string, planID int64, now time.Time) (*coupon.Coupon, error) {
	if code == "" {
		return nil, nil
	}
	found, err := coupons.FindOneByCode(ctx, code)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.CouponNotExist, "coupon not found")
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find coupon %q", code)
	}
	if err := CheckCoupon(found, planID, now); err != nil {
		return nil, err
	}
	return found, nil
}

// CheckCoupon reports why c cannot discount an order for planID at now, or
// nil when it can. Start and expire times are Unix milliseconds.
func CheckCoupon(c *coupon.Coupon, planID int64, now time.Time) error {
	if !c.IsEnabled() {
		return xerr.Errorf(xerr.CouponDisabled, "coupon disabled")
	}
	nowMilli := now.UnixMilli()
	if c.StartTime > 0 && nowMilli < c.StartTime {
		return xerr.Errorf(xerr.CouponNotApplicable, "coupon is not active")
	}
	if c.ExpireTime <= 0 || nowMilli > c.ExpireTime {
		return xerr.Errorf(xerr.CouponExpired, "coupon expired")
	}
	if c.Count != 0 && c.Count <= c.UsedCount {
		return xerr.Errorf(xerr.CouponInsufficientUsage, "coupon used")
	}
	plans, err := slicesx.ParseInt64CSV(c.Subscribe)
	if err != nil {
		// A damaged plan list must not read as an empty one, which would
		// widen the coupon to every plan.
		return xerr.Wrapf(err, xerr.CouponNotApplicable, "coupon %d plan list: %v", c.Id, err)
	}
	if len(plans) > 0 && !slices.Contains(plans, planID) {
		return xerr.Errorf(xerr.CouponNotApplicable, "coupon not match")
	}
	return nil
}
