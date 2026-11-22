package pricing

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

func TestComputeAppliesDiscountCouponGiftAndFeeInCanonicalOrder(t *testing.T) {
	percent := func(value int64) *Coupon { return &Coupon{Percent: true, Value: value} }
	fixed := func(value int64) *Coupon { return &Coupon{Value: value} }
	tests := []struct {
		name string
		in   Input
		want Quote
	}{
		{"list price", Input{UnitPrice: 1000, Quantity: 1},
			Quote{Price: 1000, Amount: 1000}},
		{"quantity multiplies the unit price", Input{UnitPrice: 1000, Quantity: 3},
			Quote{Price: 3000, Amount: 3000}},
		{"tier discount", Input{UnitPrice: 1000, Quantity: 3, Tiers: []Tier{{Quantity: 3, Discount: 90}}},
			Quote{Price: 3000, Discount: 300, Amount: 2700}},
		{"tier not reached", Input{UnitPrice: 1000, Quantity: 2, Tiers: []Tier{{Quantity: 3, Discount: 90}}},
			Quote{Price: 2000, Amount: 2000}},
		{"best qualifying tier wins", Input{UnitPrice: 100, Quantity: 12, Tiers: []Tier{{Quantity: 3, Discount: 95}, {Quantity: 12, Discount: 80}, {Quantity: 24, Discount: 50}}},
			Quote{Price: 1200, Discount: 240, Amount: 960}},
		{"invalid tiers are ignored", Input{UnitPrice: 100, Quantity: 1, Tiers: []Tier{{Quantity: 0, Discount: 10}, {Quantity: 1, Discount: 150}, {Quantity: 1, Discount: -5}}},
			Quote{Price: 100, Amount: 100}},
		// 10 × 70% is 7; the float multiplication this replaced produced 6.
		{"tier percent is exact", Input{UnitPrice: 10, Quantity: 1, Tiers: []Tier{{Quantity: 1, Discount: 70}}},
			Quote{Price: 10, Discount: 3, Amount: 7}},
		{"fractional tier percent", Input{UnitPrice: 1000, Quantity: 1, Tiers: []Tier{{Quantity: 1, Discount: 92.5}}},
			Quote{Price: 1000, Discount: 75, Amount: 925}},
		{"tier fraction of a cent is dropped", Input{UnitPrice: 333, Quantity: 1, Tiers: []Tier{{Quantity: 1, Discount: 50}}},
			Quote{Price: 333, Discount: 167, Amount: 166}},
		{"percent coupon after the tier", Input{UnitPrice: 1000, Quantity: 3, Tiers: []Tier{{Quantity: 3, Discount: 90}}, Coupon: percent(10)},
			Quote{Price: 3000, Discount: 300, CouponDiscount: 270, Amount: 2430}},
		{"percent coupon over 100 covers the amount", Input{UnitPrice: 1000, Quantity: 1, Coupon: percent(150)},
			Quote{Price: 1000, CouponDiscount: 1000}},
		{"fixed coupon", Input{UnitPrice: 1000, Quantity: 1, Coupon: fixed(250)},
			Quote{Price: 1000, CouponDiscount: 250, Amount: 750}},
		{"fixed coupon is capped at the amount", Input{UnitPrice: 300, Quantity: 1, Coupon: fixed(500)},
			Quote{Price: 300, CouponDiscount: 300}},
		{"negative coupon grants nothing", Input{UnitPrice: 300, Quantity: 1, Coupon: fixed(-5)},
			Quote{Price: 300, Amount: 300}},
		{"gift credit after the coupon", Input{UnitPrice: 1000, Quantity: 1, Coupon: fixed(100), GiftCredit: 400},
			Quote{Price: 1000, CouponDiscount: 100, GiftAmount: 400, Amount: 500}},
		{"gift credit is capped at the amount", Input{UnitPrice: 1000, Quantity: 1, GiftCredit: 5000},
			Quote{Price: 1000, GiftAmount: 1000}},
		// The case that used to disagree: the preview charged the fee on the
		// gift-covered part (700) while the renewal order did not (660).
		{"fee only on the part paid through the gateway", Input{UnitPrice: 1000, Quantity: 1, GiftCredit: 400, Fee: Fee{Mode: FeePercent, Percent: 10}},
			Quote{Price: 1000, GiftAmount: 400, FeeAmount: 60, Amount: 660}},
		{"no fee when the gift covers everything", Input{UnitPrice: 1000, Quantity: 1, GiftCredit: 1000, Fee: Fee{Mode: FeePercentPlusFixed, Percent: 10, Fixed: 50}},
			Quote{Price: 1000, GiftAmount: 1000}},
		{"no fee when the coupon covers everything", Input{UnitPrice: 1000, Quantity: 1, Coupon: percent(100), Fee: Fee{Mode: FeeFixed, Fixed: 50}},
			Quote{Price: 1000, CouponDiscount: 1000}},
		{"fixed fee", Input{UnitPrice: 100, Quantity: 1, Fee: Fee{Mode: FeeFixed, Fixed: 50}},
			Quote{Price: 100, FeeAmount: 50, Amount: 150}},
		{"percent plus fixed fee", Input{UnitPrice: 1000, Quantity: 1, Fee: Fee{Mode: FeePercentPlusFixed, Percent: 3, Fixed: 20}},
			Quote{Price: 1000, FeeAmount: 50, Amount: 1050}},
		{"percent fee fraction is dropped", Input{UnitPrice: 999, Quantity: 1, Fee: Fee{Mode: FeePercent, Percent: 3}},
			Quote{Price: 999, FeeAmount: 29, Amount: 1028}},
		{"disabled fee", Input{UnitPrice: 1000, Quantity: 1, Fee: Fee{Mode: FeeNone, Percent: 10, Fixed: 10}},
			Quote{Price: 1000, Amount: 1000}},
		{"unknown fee mode", Input{UnitPrice: 1000, Quantity: 1, Fee: Fee{Mode: 9, Percent: 10}},
			Quote{Price: 1000, Amount: 1000}},
		{"negative fee terms charge nothing", Input{UnitPrice: 1000, Quantity: 1, Fee: Fee{Mode: FeePercentPlusFixed, Percent: -1, Fixed: 10}},
			Quote{Price: 1000, Amount: 1000}},
		{"everything combined", Input{
			UnitPrice: 2000, Quantity: 6,
			Tiers:      []Tier{{Quantity: 6, Discount: 85}},
			Coupon:     percent(10),
			GiftCredit: 1000,
			Fee:        Fee{Mode: FeePercentPlusFixed, Percent: 2, Fixed: 30},
		}, Quote{Price: 12000, Discount: 1800, CouponDiscount: 1020, GiftAmount: 1000, FeeAmount: 193, Amount: 8373}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compute(tt.in); got != tt.want {
				t.Fatalf("Compute(%+v)\n got %+v\nwant %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestComputeSaturatesInsteadOfOverflowing(t *testing.T) {
	quote := Compute(Input{UnitPrice: math.MaxInt64 / 2, Quantity: 1000})
	if quote.Price != math.MaxInt64 || quote.Amount != math.MaxInt64 {
		t.Fatalf("overflowing price = %+v, want saturation", quote)
	}
}

func TestTermsFromEntities(t *testing.T) {
	if terms := CouponTerms(&coupon.Coupon{Type: coupon.TypePercentage, Discount: 15}); *terms != (Coupon{Percent: true, Value: 15}) {
		t.Fatalf("percentage coupon terms = %+v", terms)
	}
	if terms := CouponTerms(&coupon.Coupon{Type: coupon.TypeFixed, Discount: 300}); *terms != (Coupon{Value: 300}) {
		t.Fatalf("fixed coupon terms = %+v", terms)
	}
	if CouponTerms(nil) != nil {
		t.Fatal("no coupon must carry no terms")
	}
	fee := FeeTerms(&payment.Payment{FeeMode: FeePercentPlusFixed, FeePercent: 3, FeeAmount: 20})
	if fee != (Fee{Mode: FeePercentPlusFixed, Percent: 3, Fixed: 20}) {
		t.Fatalf("fee terms = %+v", fee)
	}
	if FeeTerms(nil) != (Fee{}) {
		t.Fatal("no payment method charges no fee")
	}
}

func TestParseTiers(t *testing.T) {
	tiers := ParseTiers(`[{"quantity":3,"discount":90},{"quantity":12,"discount":72.5}]`)
	if len(tiers) != 2 || tiers[1] != (Tier{Quantity: 12, Discount: 72.5}) {
		t.Fatalf("tiers = %+v", tiers)
	}
	if ParseTiers("") != nil || ParseTiers("not json") != nil {
		t.Fatal("empty or malformed tables grant no discount")
	}
}

// Coupon start and expire times are Unix milliseconds. Comparing them with a
// seconds clock made every coupon with a start time permanently inactive.
func TestCheckCoupon(t *testing.T) {
	enabled, disabled := true, false
	now := time.UnixMilli(1_800_000_000_000)
	hour := time.Hour.Milliseconds()
	valid := func() *coupon.Coupon {
		return &coupon.Coupon{Enable: &enabled, StartTime: now.UnixMilli() - hour, ExpireTime: now.UnixMilli() + hour, Count: 5, UsedCount: 1, Subscribe: "3,9"}
	}
	tests := []struct {
		name   string
		mutate func(*coupon.Coupon)
		plan   int64
		want   uint32
	}{
		{"usable", func(*coupon.Coupon) {}, 9, 0},
		{"unrestricted plans", func(c *coupon.Coupon) { c.Subscribe = "" }, 42, 0},
		{"unlimited count", func(c *coupon.Coupon) { c.Count, c.UsedCount = 0, 99 }, 9, 0},
		{"disabled", func(c *coupon.Coupon) { c.Enable = &disabled }, 9, xerr.CouponDisabled},
		{"not yet started", func(c *coupon.Coupon) { c.StartTime = now.UnixMilli() + hour }, 9, xerr.CouponNotApplicable},
		{"expired", func(c *coupon.Coupon) { c.ExpireTime = now.UnixMilli() - 1 }, 9, xerr.CouponExpired},
		{"no expiry configured", func(c *coupon.Coupon) { c.ExpireTime = 0 }, 9, xerr.CouponExpired},
		{"used up", func(c *coupon.Coupon) { c.UsedCount = 5 }, 9, xerr.CouponInsufficientUsage},
		{"other plan", func(*coupon.Coupon) {}, 4, xerr.CouponNotApplicable},
		// A damaged plan list must not read as "every plan".
		{"damaged plan list", func(c *coupon.Coupon) { c.Subscribe = "3,x" }, 42, xerr.CouponNotApplicable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(c)
			err := CheckCoupon(c, tt.plan, now)
			if tt.want == 0 {
				if err != nil {
					t.Fatalf("CheckCoupon = %v, want usable", err)
				}
				return
			}
			if got := xerr.CodeOf(err); err == nil || got != tt.want {
				t.Fatalf("CheckCoupon = %v (code %d), want code %d", err, got, tt.want)
			}
		})
	}
}

type couponLookup struct {
	coupon *coupon.Coupon
	err    error
}

func (l couponLookup) FindOneByCode(context.Context, string) (*coupon.Coupon, error) {
	return l.coupon, l.err
}

func TestResolveCouponMapsLookupFailures(t *testing.T) {
	enabled := true
	now := time.Now()
	ctx := context.Background()
	if c, err := ResolveCoupon(ctx, couponLookup{}, "", 1, now); c != nil || err != nil {
		t.Fatalf("no code = (%v, %v), want no coupon", c, err)
	}
	if _, err := ResolveCoupon(ctx, couponLookup{err: gorm.ErrRecordNotFound}, "NOPE", 1, now); xerr.CodeOf(err) != xerr.CouponNotExist {
		t.Fatalf("missing coupon = %v, want CouponNotExist", err)
	}
	if _, err := ResolveCoupon(ctx, couponLookup{err: gorm.ErrInvalidDB}, "ANY", 1, now); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("lookup failure = %v, want DatabaseQueryError", err)
	}
	found := &coupon.Coupon{Code: "OK", Enable: &enabled, ExpireTime: now.Add(time.Hour).UnixMilli()}
	if c, err := ResolveCoupon(ctx, couponLookup{coupon: found}, "OK", 1, now); c != found || err != nil {
		t.Fatalf("usable coupon = (%v, %v)", c, err)
	}
}
