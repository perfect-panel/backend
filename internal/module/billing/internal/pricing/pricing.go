// Package pricing decides what an order costs. Every order creator and every
// price preview goes through Compute, so a preview always equals the order it
// previews. Amounts are int64 minor units; percentages are applied with exact
// integer arithmetic and the fraction of a minor unit is dropped in the
// buyer's favour.
package pricing

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

// Tier is one row of a plan's quantity discount table: buying at least
// Quantity units pays Discount percent (0–100) of the price.
type Tier struct {
	Quantity int64   `json:"quantity"`
	Discount float64 `json:"discount"`
}

// ParseTiers decodes a plan's quantity discount table. An empty or malformed
// table grants no discount.
func ParseTiers(raw string) []Tier {
	if raw == "" {
		return nil
	}
	var tiers []Tier
	if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
		return nil
	}
	return tiers
}

// Coupon is the discount a coupon grants: Percent coupons take Value percent
// of the amount, fixed coupons take Value minor units.
type Coupon struct {
	Percent bool
	Value   int64
}

// CouponTerms returns the discount terms of c, or nil for no coupon.
func CouponTerms(c *coupon.Coupon) *Coupon {
	if c == nil {
		return nil
	}
	return &Coupon{Percent: c.Type == coupon.TypePercentage, Value: c.Discount}
}

// Fee modes of a payment method.
const (
	FeeNone             uint = 0
	FeePercent          uint = 1
	FeeFixed            uint = 2
	FeePercentPlusFixed uint = 3
)

// Fee is the handling fee a payment method charges on the amount paid
// through it.
type Fee struct {
	Mode    uint
	Percent int64
	Fixed   int64
}

// FeeTerms returns the handling fee of the payment method; nil charges none.
func FeeTerms(method *payment.Payment) Fee {
	if method == nil {
		return Fee{}
	}
	return Fee{Mode: method.FeeMode, Percent: method.FeePercent, Fixed: method.FeeAmount}
}

// Input describes an order to price.
type Input struct {
	UnitPrice int64
	Quantity  int64
	Tiers     []Tier
	Coupon    *Coupon
	// GiftCredit is the gift balance the buyer may spend on the order.
	GiftCredit int64
	Fee        Fee
}

// Quote is the price of an order and how it is paid. Amount is what remains
// to be paid through the payment method.
type Quote struct {
	Price          int64
	Discount       int64
	CouponDiscount int64
	GiftAmount     int64
	FeeAmount      int64
	Amount         int64
}

// Subtotal is the price after the quantity discount, before the coupon.
func (q Quote) Subtotal() int64 { return q.Price - q.Discount }

// Compute prices an order in the one canonical order: the quantity discount,
// then the coupon, then the gift credit, and finally the payment method's fee
// on what remains to be paid through it. The part the gift credit covers
// carries no fee, and nothing is charged when nothing remains.
func Compute(in Input) Quote {
	q := Quote{Price: multiply(in.UnitPrice, in.Quantity)}
	discounted := applyTiers(q.Price, in.Quantity, in.Tiers)
	q.Discount = q.Price - discounted
	q.CouponDiscount = couponDiscount(discounted, in.Coupon)
	due := discounted - q.CouponDiscount
	if in.GiftCredit > 0 && due > 0 {
		q.GiftAmount = min(in.GiftCredit, due)
	}
	remaining := due - q.GiftAmount
	q.FeeAmount = fee(remaining, in.Fee)
	q.Amount = remaining + q.FeeAmount
	return q
}

// applyTiers returns the price after the best quantity discount the order
// qualifies for.
func applyTiers(price, quantity int64, tiers []Tier) int64 {
	best := 100.0
	for _, tier := range tiers {
		if tier.Quantity > 0 && tier.Discount >= 0 && tier.Discount <= 100 && quantity >= tier.Quantity && tier.Discount < best {
			best = tier.Discount
		}
	}
	if best == 100 || price <= 0 {
		return price
	}
	// The percentage is taken at its shortest decimal form, the value an
	// administrator entered.
	percent, ok := new(big.Rat).SetString(strconv.FormatFloat(best, 'f', -1, 64))
	if !ok {
		return price
	}
	return percentOf(price, percent)
}

func couponDiscount(amount int64, terms *Coupon) int64 {
	if amount <= 0 || terms == nil || terms.Value < 0 {
		return 0
	}
	if !terms.Percent {
		return min(terms.Value, amount)
	}
	if terms.Value > 100 {
		return amount
	}
	return percentOf(amount, new(big.Rat).SetInt64(terms.Value))
}

func fee(amount int64, terms Fee) int64 {
	if amount <= 0 || terms.Percent < 0 || terms.Fixed < 0 {
		return 0
	}
	switch terms.Mode {
	case FeePercent:
		return percentOf(amount, new(big.Rat).SetInt64(terms.Percent))
	case FeeFixed:
		return terms.Fixed
	case FeePercentPlusFixed:
		return add(percentOf(amount, new(big.Rat).SetInt64(terms.Percent)), terms.Fixed)
	default:
		return 0
	}
}

// percentOf returns percent percent of a non-negative amount, rounded down to
// the minor unit.
func percentOf(amount int64, percent *big.Rat) int64 {
	exact := new(big.Rat).Mul(new(big.Rat).SetInt64(amount), percent)
	exact.Quo(exact, big.NewRat(100, 1))
	result := new(big.Int).Quo(exact.Num(), exact.Denom())
	if !result.IsInt64() {
		return math.MaxInt64
	}
	return result.Int64()
}

// multiply and add saturate instead of overflowing, so an absurd plan price
// fails the order amount limit rather than wrapping around to a small one.
func multiply(a, b int64) int64 {
	product := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	switch {
	case product.IsInt64():
		return product.Int64()
	case product.Sign() > 0:
		return math.MaxInt64
	default:
		return math.MinInt64
	}
}

func add(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
