package checkout

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// pricingCase is one combination of the price components.
type pricingCase struct {
	name      string
	unitPrice int64
	quantity  int64
	tiers     string
	coupon    *coupon.Coupon
	gift      int64
	fee       func(*payment.Payment)
	want      pricing.Quote
}

func pricingCases() []pricingCase {
	fixedFee := func(p *payment.Payment) { p.FeeMode, p.FeeAmount = 2, 30 }
	mixedFee := func(p *payment.Payment) { p.FeeMode, p.FeePercent, p.FeeAmount = 3, 5, 20 }
	percentCoupon := &coupon.Coupon{Type: coupon.TypePercentage, Discount: 10}
	fixedCoupon := &coupon.Coupon{Type: coupon.TypeFixed, Discount: 150}
	return []pricingCase{
		{name: "list price", unitPrice: 1000, quantity: 1,
			want: pricing.Quote{Price: 1000, Amount: 1000}},
		{name: "quantity tier", unitPrice: 1000, quantity: 3, tiers: `[{"quantity":3,"discount":90}]`,
			want: pricing.Quote{Price: 3000, Discount: 300, Amount: 2700}},
		{name: "percent coupon", unitPrice: 1000, quantity: 3, tiers: `[{"quantity":3,"discount":90}]`, coupon: percentCoupon,
			want: pricing.Quote{Price: 3000, Discount: 300, CouponDiscount: 270, Amount: 2430}},
		{name: "fixed coupon", unitPrice: 1000, quantity: 1, coupon: fixedCoupon,
			want: pricing.Quote{Price: 1000, CouponDiscount: 150, Amount: 850}},
		{name: "gift credit", unitPrice: 1000, quantity: 1, gift: 400,
			want: pricing.Quote{Price: 1000, GiftAmount: 400, Amount: 600}},
		// The reported disagreement: the preview charged 700 (fee on the
		// full amount), the renewal order 660.
		{name: "fee after the gift credit", unitPrice: 1000, quantity: 1, gift: 400, fee: percentFee(10),
			want: pricing.Quote{Price: 1000, GiftAmount: 400, FeeAmount: 60, Amount: 660}},
		{name: "no fee when the gift covers everything", unitPrice: 1000, quantity: 1, gift: 5000, fee: fixedFee,
			want: pricing.Quote{Price: 1000, GiftAmount: 1000}},
		{name: "everything", unitPrice: 2000, quantity: 6, tiers: `[{"quantity":6,"discount":85}]`, coupon: percentCoupon, gift: 1000, fee: mixedFee,
			// 12000 → 10200 → coupon 1020 → 9180 → gift 1000 → 8180 → fee 409 + 20
			want: pricing.Quote{Price: 12000, Discount: 1800, CouponDiscount: 1020, GiftAmount: 1000, FeeAmount: 429, Amount: 8609}},
	}
}

func (f *checkoutFixture) pricingTerms(tc pricingCase) (*subscribe.Subscribe, *payment.Payment, string) {
	f.t.Helper()
	plan := f.h.Plan(tc.unitPrice, func(p *subscribe.Subscribe) { p.Discount = tc.tiers })
	var adjust []func(*payment.Payment)
	if tc.fee != nil {
		adjust = append(adjust, tc.fee)
	}
	method := f.epay(adjust...)
	code := ""
	if tc.coupon != nil {
		code = "C-" + plan.Name
		f.h.Coupon(code, func(c *coupon.Coupon) { c.Type, c.Discount = tc.coupon.Type, tc.coupon.Discount })
	}
	return plan, method, code
}

func assertOrderMatchesQuote(t *testing.T, o *order.Order, want pricing.Quote) {
	t.Helper()
	got := pricing.Quote{Price: o.Price, Discount: o.Discount, CouponDiscount: o.CouponDiscount, GiftAmount: o.GiftAmount, FeeAmount: o.FeeAmount, Amount: o.Amount}
	if got != want {
		t.Fatalf("order prices\n got %+v\nwant %+v", got, want)
	}
}

func assertPreviewMatchesQuote(t *testing.T, preview *dto.PreOrderResponse, want pricing.Quote) {
	t.Helper()
	got := pricing.Quote{Price: preview.Price, Discount: preview.Discount, CouponDiscount: preview.CouponDiscount, GiftAmount: preview.GiftAmount, FeeAmount: preview.FeeAmount, Amount: preview.Amount}
	if got != want {
		t.Fatalf("preview\n got %+v\nwant %+v", got, want)
	}
}

// A purchase is priced exactly as its preview showed, for every combination
// of discount, coupon, gift credit and fee.
func TestPurchasePreviewEqualsTheCreatedOrder(t *testing.T) {
	for _, tc := range pricingCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, ctx := f.buyer(tc.gift)
			plan, method, code := f.pricingTerms(tc)
			req := &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: tc.quantity, Payment: method.Id, Coupon: code}

			preview, err := f.svc.PreCreateOrder(ctx, req)
			if err != nil {
				t.Fatalf("PreCreateOrder: %v", err)
			}
			assertPreviewMatchesQuote(t, preview, tc.want)
			resp, err := f.svc.Purchase(ctx, req)
			if err != nil {
				t.Fatalf("Purchase: %v", err)
			}
			created := f.h.ReloadOrder(resp.OrderNo)
			assertOrderMatchesQuote(t, created, tc.want)
			if created.Type != order.TypeSubscribe || created.Status != order.StatusPending || created.Method != "EPay" {
				t.Fatalf("created order = %+v", created)
			}
			assertGiftSpent(t, f, u.Id, tc, ledger.RemarkPurchaseDeduction, resp.OrderNo)
		})
	}
}

// A renewal is priced like the preview of the subscription it renews.
func TestRenewalPreviewEqualsTheCreatedOrder(t *testing.T) {
	for _, tc := range pricingCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, ctx := f.buyer(tc.gift)
			plan, method, code := f.pricingTerms(tc)
			sub := f.h.UserSubscription(u.Id, plan)

			preview, err := f.svc.PreCreateOrder(ctx, &dto.PurchaseOrderRequest{
				SubscribeId: plan.Id, UserSubscribeId: sub.Id, Quantity: tc.quantity, Payment: method.Id, Coupon: code,
			})
			if err != nil {
				t.Fatalf("PreCreateOrder: %v", err)
			}
			assertPreviewMatchesQuote(t, preview, tc.want)
			resp, err := f.svc.Renewal(ctx, &dto.RenewalOrderRequest{UserSubscribeID: sub.Id, Quantity: tc.quantity, Payment: method.Id, Coupon: code})
			if err != nil {
				t.Fatalf("Renewal: %v", err)
			}
			created := f.h.ReloadOrder(resp.OrderNo)
			assertOrderMatchesQuote(t, created, tc.want)
			if created.Type != order.TypeRenewal || created.SubscribeToken != sub.Token {
				t.Fatalf("created order = %+v", created)
			}
			assertGiftSpent(t, f, u.Id, tc, ledger.RemarkRenewalDeduction, resp.OrderNo)
		})
	}
}

// A traffic reset pays the replacement price in the same canonical order,
// and its gift ledger entry names the traffic reset.
func TestResetTrafficIsPricedCanonically(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(400)
	plan := f.h.Plan(5000, func(p *subscribe.Subscribe) { p.Replacement = 1000 })
	sub := f.h.UserSubscription(u.Id, plan)
	method := f.epay(percentFee(10))

	resp, err := f.svc.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{UserSubscribeID: sub.Id, Payment: method.Id})
	if err != nil {
		t.Fatalf("ResetTraffic: %v", err)
	}
	created := f.h.ReloadOrder(resp.OrderNo)
	assertOrderMatchesQuote(t, created, pricing.Quote{Price: 1000, GiftAmount: 400, FeeAmount: 60, Amount: 660})
	if created.Type != order.TypeResetTraffic || created.SubscribeToken != sub.Token {
		t.Fatalf("created order = %+v", created)
	}
	gifts := f.h.GiftLogs(u.Id)
	if len(gifts) != 1 || gifts[0].Remark != ledger.RemarkResetTrafficDeduction || gifts[0].Amount != 400 || gifts[0].Timestamp == 0 {
		t.Fatalf("gift ledger = %+v, want the reset traffic deduction", gifts)
	}
}

// A recharge carries the recharged amount as its price and the fee on top.
func TestRechargeChargesTheFeeOnTop(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(900)
	method := f.epay(percentFee(3))

	resp, err := f.svc.Recharge(ctx, &dto.RechargeOrderRequest{Amount: 1990, Payment: method.Id})
	if err != nil {
		t.Fatalf("Recharge: %v", err)
	}
	created := f.h.ReloadOrder(resp.OrderNo)
	// Gift credit never pays a recharge.
	assertOrderMatchesQuote(t, created, pricing.Quote{Price: 1990, FeeAmount: 59, Amount: 2049})
	if created.Type != order.TypeRecharge || f.h.ReloadWallet(u.Id).GiftAmount != 900 {
		t.Fatalf("created order = %+v", created)
	}
}

// The balance checkout spends gift credit first, so a balance-paid top-up
// would convert gift credit into regular balance.
func TestRechargeRejectsBalancePayment(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(0)
	balance := f.h.Payment("balance", "")
	_, err := f.svc.Recharge(ctx, &dto.RechargeOrderRequest{Amount: 1000, Payment: balance.Id})
	assertCode(t, err, xerr.PaymentMethodNotFound)
	if len(f.h.Orders(u.Id)) != 0 {
		t.Fatal("a balance-paid recharge order was created")
	}
}

func assertGiftSpent(t *testing.T, f *checkoutFixture, userID int64, tc pricingCase, remark, orderNo string) {
	t.Helper()
	if got := f.h.ReloadWallet(userID).GiftAmount; got != tc.gift-tc.want.GiftAmount {
		t.Fatalf("remaining gift credit = %d, want %d", got, tc.gift-tc.want.GiftAmount)
	}
	gifts := f.h.GiftLogs(userID)
	if tc.want.GiftAmount == 0 {
		if len(gifts) != 0 {
			t.Fatalf("gift ledger = %+v, want no movement", gifts)
		}
		return
	}
	if len(gifts) != 1 || gifts[0].Remark != remark || gifts[0].Amount != tc.want.GiftAmount ||
		gifts[0].OrderNo != orderNo || gifts[0].Timestamp == 0 {
		t.Fatalf("gift ledger = %+v, want one %q entry", gifts, remark)
	}
}

// Every flow reports a missing payment method the same way.
func TestOrderFlowsReportAMissingPaymentMethodAlike(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(0)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Replacement = 100 })
	sub := f.h.UserSubscription(u.Id, plan)
	const missing = 404
	for name, create := range map[string]func() error{
		"purchase": func() error {
			_, err := f.svc.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: missing})
			return err
		},
		"renewal": func() error {
			_, err := f.svc.Renewal(ctx, &dto.RenewalOrderRequest{UserSubscribeID: sub.Id, Quantity: 1, Payment: missing})
			return err
		},
		"reset traffic": func() error {
			_, err := f.svc.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{UserSubscribeID: sub.Id, Payment: missing})
			return err
		},
		"recharge": func() error {
			_, err := f.svc.Recharge(ctx, &dto.RechargeOrderRequest{Amount: 1000, Payment: missing})
			return err
		},
		"preview": func() error {
			_, err := f.svc.PreCreateOrder(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: missing})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { assertCode(t, create(), xerr.PaymentMethodNotFound) })
	}
	if len(f.h.Orders(u.Id)) != 0 {
		t.Fatal("an order was created without a payment method")
	}
}

// A used-up coupon is reported with one code by the preview and the order.
func TestUsedUpCouponIsReportedAlikeByPreviewAndOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	_, ctx := f.buyer(0)
	plan := f.h.Plan(1000)
	method := f.epay()
	f.h.Coupon("GONE", func(c *coupon.Coupon) { c.Count, c.UsedCount = 1, 1 })
	req := &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "GONE"}

	_, err := f.svc.PreCreateOrder(ctx, req)
	assertCode(t, err, xerr.CouponInsufficientUsage)
	_, err = f.svc.Purchase(ctx, req)
	assertCode(t, err, xerr.CouponInsufficientUsage)
}

// raceTransactor commits a competing order for the coupon just before the
// order transaction runs, standing in for a concurrent request that held
// the buyer's wallet lock first.
type raceTransactor struct {
	tx      Transactor
	compete func()
}

var _ Transactor = raceTransactor{}

func (r raceTransactor) InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error {
	r.compete()
	return r.tx.InBillingTx(ctx, fn)
}

// Concurrent orders all pass the per-user coupon count taken before the order
// transaction; the count must be repeated under the buyer's wallet lock.
func TestCheckoutRechecksCouponUserLimitUnderWalletLock(t *testing.T) {
	for _, flow := range []string{"purchase", "renewal"} {
		t.Run(flow, func(t *testing.T) {
			for _, limit := range []int64{1, 2} {
				f := newCheckoutFixture(t)
				u, ctx := f.buyer(0)
				plan := f.h.Plan(1000)
				sub := f.h.UserSubscription(u.Id, plan)
				method := f.epay()
				f.h.Coupon("ONCE", func(c *coupon.Coupon) { c.UserLimit = limit })
				f.svc.deps.Tx = raceTransactor{tx: f.h.Store, compete: func() {
					f.h.Order(&order.Order{OrderNo: "competing-" + flow, UserId: u.Id, Coupon: "ONCE", Status: order.StatusPending})
				}}
				var err error
				if flow == "purchase" {
					_, err = f.svc.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "ONCE"})
				} else {
					_, err = f.svc.Renewal(ctx, &dto.RenewalOrderRequest{UserSubscribeID: sub.Id, Quantity: 1, Payment: method.Id, Coupon: "ONCE"})
				}
				used := f.h.ReloadCoupon("ONCE").UsedCount
				if limit == 1 {
					assertCode(t, err, xerr.CouponInsufficientUsage)
					if len(f.h.Orders(u.Id)) != 1 || used != 0 {
						t.Fatalf("over-limit order created: orders=%d coupon uses=%d", len(f.h.Orders(u.Id)), used)
					}
					continue
				}
				if err != nil || len(f.h.Orders(u.Id)) != 2 || used != 1 {
					t.Fatalf("order below the per-user limit: err=%v orders=%d coupon uses=%d", err, len(f.h.Orders(u.Id)), used)
				}
			}
		})
	}
}
