package portal

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ------------------------------------------------------ balance payment

// The wallet pays gift credit first. The credit it consumes moves from the
// amount paid with money to the order's gift credit, alongside what it
// reserved at creation, so the order's columns keep their meaning (Amount
// paid with money, GiftAmount consumed gift credit) and a refund of both
// returns exactly what was paid; each movement is recorded.
func TestBalanceCheckoutSpendsGiftCreditFirst(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(2300, 200)
	o := f.pendingOrder("order-1", u.Id, 2500, f.balance(), func(o *order.Order) { o.GiftAmount = 300 })

	resp, err := f.checkout(ctx, o.OrderNo, "")
	if err != nil || resp.Type != "balance" {
		t.Fatalf("Checkout = (%+v, %v), want a balance payment", resp, err)
	}
	if w := f.h.ReloadWallet(u.Id); w.Balance != 0 || w.GiftAmount != 0 {
		t.Fatalf("wallet = %+v, want emptied", w)
	}
	paid := f.h.ReloadOrder(o.OrderNo)
	if paid.Status != order.StatusPaid || paid.GiftAmount != 500 || paid.Amount != 2300 {
		t.Fatalf("order = %+v, want paid with 2300 of balance and 500 of gift credit", paid)
	}
	gifts := f.h.GiftLogs(u.Id)
	if len(gifts) != 1 || gifts[0].Amount != 200 || gifts[0].Balance != 0 || gifts[0].Remark != ledger.RemarkBalancePayment || gifts[0].Timestamp == 0 {
		t.Fatalf("gift ledger = %+v", gifts)
	}
	balances := f.h.BalanceLogs(u.Id)
	if len(balances) != 1 || balances[0].Amount != 2300 || balances[0].Balance != 0 || balances[0].OrderNo != o.OrderNo || balances[0].Timestamp == 0 {
		t.Fatalf("balance ledger = %+v", balances)
	}
	if len(f.queue.Activations) != 1 || f.queue.Activations[0] != o.OrderNo {
		t.Fatalf("activations = %v, want the order once", f.queue.Activations)
	}
}

func TestBalanceCheckoutRejectsInsufficientBalance(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(500, 0)
	o := f.pendingOrder("order-1", u.Id, 2500, f.balance())

	_, err := f.checkout(ctx, o.OrderNo, "")
	assertCode(t, err, xerr.InsufficientBalance)
	if w := f.h.ReloadWallet(u.Id); w.Balance != 500 || f.h.ReloadOrder(o.OrderNo).Status != order.StatusPending ||
		len(f.h.BalanceLogs(u.Id)) != 0 || len(f.queue.Activations) != 0 {
		t.Fatal("a refused balance payment changed the wallet or the order")
	}
}

// A repeated checkout of a paid order must not debit the wallet again.
func TestBalanceCheckoutDebitsOnce(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(5000, 0)
	o := f.pendingOrder("order-1", u.Id, 2500, f.balance())

	if _, err := f.checkout(ctx, o.OrderNo, ""); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	_, err := f.checkout(ctx, o.OrderNo, "")
	assertCode(t, err, xerr.OrderStatusError)
	if w := f.h.ReloadWallet(u.Id); w.Balance != 2500 || len(f.h.BalanceLogs(u.Id)) != 1 {
		t.Fatalf("wallet = %+v, want one debit", w)
	}
}

// Two checkouts can both read the order as pending; the order row lock lets
// only the first debit the wallet.
func TestBalanceCheckoutRechecksTheOrderUnderItsLock(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(5000, 0)
	o := f.pendingOrder("order-1", u.Id, 2500, f.balance())
	f.svc.deps.Tx = raceTransactor{tx: f.h.Store, compete: func() {
		if _, err := f.h.Store.Order().UpdateOrderStatusFrom(context.Background(), o.OrderNo, order.StatusPending, order.StatusPaid); err != nil {
			t.Fatal(err)
		}
	}}

	_, err := f.checkout(ctx, o.OrderNo, "")
	assertCode(t, err, xerr.OrderStatusError)
	if f.h.ReloadWallet(u.Id).Balance != 5000 {
		t.Fatal("the wallet paid an order another checkout already paid")
	}
}

// Once the debit committed the order is paid; an activation that cannot be
// queued is re-driven by the paid-order reconciler, so the buyer is told
// the committed outcome instead of an error inviting a second payment.
func TestBalanceCheckoutReportsTheCommittedPaymentWhenActivationCannotBeQueued(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(2500, 0)
	o := f.pendingOrder("order-1", u.Id, 2500, f.balance())
	f.queue.ActivationErr = errors.New("queue unavailable")

	resp, err := f.checkout(ctx, o.OrderNo, "")
	if err != nil || resp.Type != "balance" {
		t.Fatalf("Checkout = (%+v, %v), want the committed payment reported", resp, err)
	}
	if f.h.ReloadOrder(o.OrderNo).Status != order.StatusPaid || f.h.ReloadWallet(u.Id).Balance != 0 {
		t.Fatal("the payment was not committed")
	}
}

// Recharge orders created before recharge rejected the balance method, and
// orders an administrator created, must not be paid from the wallet.
func TestBalanceCheckoutRejectsRechargeOrder(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(5000, 700)
	o := f.pendingOrder("recharge-1", u.Id, 1000, f.balance(), func(o *order.Order) { o.Type = order.TypeRecharge })

	_, err := f.checkout(ctx, o.OrderNo, "")
	assertCode(t, err, xerr.PaymentMethodNotFound)
	if w := f.h.ReloadWallet(u.Id); w.Balance != 5000 || w.GiftAmount != 700 {
		t.Fatal("the wallet was debited for a recharge order")
	}
}

func TestBalanceCheckoutPaysAFreeOrderWithoutTheWallet(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(100, 100)
	o := f.pendingOrder("free-1", u.Id, 0, f.balance())

	if _, err := f.checkout(ctx, o.OrderNo, ""); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if f.h.ReloadOrder(o.OrderNo).Status != order.StatusPaid || len(f.queue.Activations) != 1 {
		t.Fatal("the free order was not paid and activated")
	}
	if w := f.h.ReloadWallet(u.Id); w.Balance != 100 || w.GiftAmount != 100 || len(f.h.GiftLogs(u.Id)) != 0 {
		t.Fatal("a free order touched the wallet")
	}
}

// -------------------------------------------------------- authorization

func TestCheckoutOfAMissingOrder(t *testing.T) {
	f := newPortalFixture(t)
	_, err := f.checkout(context.Background(), "missing", "")
	assertCode(t, err, xerr.OrderNotExist)
}

func TestCheckoutRequiresTheOrderOwner(t *testing.T) {
	f := newPortalFixture(t)
	u, _ := f.buyer(5000, 0)
	o := f.pendingOrder("order-1", u.Id, 1000, f.balance())
	for name, ctx := range map[string]context.Context{
		"anonymous":    context.Background(),
		"another user": user.NewContext(context.Background(), &user.User{Id: u.Id + 1}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.checkout(ctx, o.OrderNo, "")
			assertCode(t, err, xerr.InvalidAccess)
		})
	}
	if f.h.ReloadWallet(u.Id).Balance != 5000 {
		t.Fatal("an unauthorized checkout debited the wallet")
	}
}

func TestGuestCheckoutRequiresTheOrderCapability(t *testing.T) {
	f := newPortalFixture(t)
	o := f.pendingOrder("guest-1", 0, 1000, f.epay(), guestToken("guest-capability"))
	for _, token := range []string{"", "wrong-capability"} {
		_, err := f.checkout(context.Background(), o.OrderNo, token)
		assertCode(t, err, xerr.InvalidAccess)
	}
	if _, err := f.checkout(context.Background(), o.OrderNo, "guest-capability"); err != nil {
		t.Fatalf("the capability holder was refused: %v", err)
	}
}

// Guest orders created before capabilities were stored on the order carry
// theirs in the temporary-order cache.
func TestGuestCheckoutAcceptsALegacyCachedCapability(t *testing.T) {
	f := newPortalFixture(t)
	o := f.pendingOrder("guest-legacy", 0, 1000, f.epay())
	info := order.TemporaryOrderInfo{OrderNo: o.OrderNo, CheckoutToken: "legacy-capability"}
	encoded, err := info.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.Redis.Set(context.Background(), fmt.Sprintf(order.TempOrderCacheKey, o.OrderNo), encoded, 0).Err(); err != nil {
		t.Fatal(err)
	}
	_, err = f.checkout(context.Background(), o.OrderNo, "wrong-capability")
	assertCode(t, err, xerr.InvalidAccess)
	if _, err := f.checkout(context.Background(), o.OrderNo, "legacy-capability"); err != nil {
		t.Fatalf("the legacy capability was refused: %v", err)
	}
}

// An order is paid with the method it was created for.
func TestCheckoutRejectsAMethodOfAnotherPlatform(t *testing.T) {
	f := newPortalFixture(t)
	u, ctx := f.buyer(5000, 0)
	o := f.pendingOrder("order-1", u.Id, 1000, f.balance(), func(o *order.Order) { o.Method = "EPay" })

	_, err := f.checkout(ctx, o.OrderNo, "")
	assertCode(t, err, xerr.PaymentMethodNotFound)
	if f.h.ReloadWallet(u.Id).Balance != 5000 {
		t.Fatal("the wallet paid an order bound to another platform")
	}
}

// ------------------------------------------------------ gateway checkout

// The notify URL is built from configuration only; the request Host header
// is chosen by the client.
func TestEPayCheckoutNotifyURLIgnoresRequestHost(t *testing.T) {
	f := newPortalFixture(t)
	method := f.epay()
	o := f.pendingOrder("guest-epay", 0, 1000, method, guestToken("guest-capability"))
	ctx := context.WithValue(context.Background(), requestctx.CtxKeyRequestHost, "attacker.example.test")

	resp, err := f.checkout(ctx, o.OrderNo, "guest-capability")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if got, want := payURLParam(t, resp, "notify_url"), "https://www.example.test/v1/notify/EPay/"+method.Token; got != want {
		t.Fatalf("notify_url = %q, want %q", got, want)
	}
}

// Without a trustworthy callback address the checkout fails before the
// payment expectation is recorded, so the order does not look as if it had
// been sent to the gateway.
func TestEPayCheckoutFailsWithoutConfiguredNotifyHost(t *testing.T) {
	f := newPortalFixture(t)
	f.siteHost = ""
	o := f.pendingOrder("guest-epay", 0, 1000, f.epay(), guestToken("guest-capability"))

	_, err := f.checkout(context.Background(), o.OrderNo, "guest-capability")
	assertCode(t, err, xerr.PaymentNotifyURLNotConfigured)
	if f.h.ReloadOrder(o.OrderNo).PaymentCurrency != "" {
		t.Fatal("a payment expectation was recorded despite the configuration error")
	}
}

// The gateway is asked for exactly the charge recorded on the order, so the
// callback it sends back always matches: the float conversion this replaced
// asked ¥0.56 while it recorded ¥0.57.
func TestGatewayIsAskedForTheRecordedCharge(t *testing.T) {
	tests := []struct {
		currency string
		rate     float64
		amount   int64
		want     int64
	}{
		{"CNY", 0, 58, 58},
		{"CNY", 0, 1990, 1990},
		{"CNY", 0, 1, 1},
		{"USD", 7.1, 58, 412},
		{"USD", 7.1, 1990, 14129},
		{"USD", 6.8932, 12345, 85097},
		{"USD", 7.25, 2, 15}, // 14.5 rounds half away from zero
		{"EUR", 7.8, 99999, 779992},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d@%v", tt.currency, tt.amount, tt.rate), func(t *testing.T) {
			f := newPortalFixture(t)
			f.currency, f.rates.rate = tt.currency, tt.rate
			o := f.pendingOrder("guest-epay", 0, tt.amount, f.epay(), guestToken("capability"))

			resp, err := f.checkout(context.Background(), o.OrderNo, "capability")
			if err != nil {
				t.Fatalf("Checkout: %v", err)
			}
			recorded := f.h.ReloadOrder(o.OrderNo)
			if recorded.PaymentAmount != tt.want || recorded.PaymentCurrency != "CNY" {
				t.Fatalf("recorded charge = %d %s, want %d CNY", recorded.PaymentAmount, recorded.PaymentCurrency, tt.want)
			}
			if sent := payURLParam(t, resp, "money"); sent != payment.FormatAmount(recorded.PaymentAmount) {
				t.Fatalf("gateway asked for %s, recorded %d", sent, recorded.PaymentAmount)
			}
		})
	}
}

// A retried checkout asks for the charge the first one recorded, even after
// the exchange rate moved, so the order's one expectation stays valid.
func TestRetriedCheckoutKeepsTheRecordedCharge(t *testing.T) {
	f := newPortalFixture(t)
	f.currency, f.rates.rate = "USD", 7.1
	o := f.pendingOrder("guest-epay", 0, 1990, f.epay(), guestToken("capability"))

	first, err := f.checkout(context.Background(), o.OrderNo, "capability")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	f.rates.Set(7.5)
	second, err := f.checkout(context.Background(), o.OrderNo, "capability")
	if err != nil {
		t.Fatalf("retried Checkout: %v", err)
	}
	if payURLParam(t, first, "money") != "141.29" || payURLParam(t, second, "money") != "141.29" {
		t.Fatalf("amounts = %s then %s, want 141.29 both times", payURLParam(t, first, "money"), payURLParam(t, second, "money"))
	}
	if recorded := f.h.ReloadOrder(o.OrderNo); recorded.PaymentAmount != 14129 {
		t.Fatalf("recorded charge = %d, want 14129", recorded.PaymentAmount)
	}
}

// Without a rate the gateway must not be sent a value merely relabelled as
// its currency.
func TestCheckoutWithoutAnExchangeRateIsRefused(t *testing.T) {
	f := newPortalFixture(t)
	f.currency = "USD"
	o := f.pendingOrder("guest-epay", 0, 1990, f.epay(), guestToken("capability"))

	if _, err := f.checkout(context.Background(), o.OrderNo, "capability"); err == nil {
		t.Fatal("the checkout relabelled USD as CNY")
	}
	if f.h.ReloadOrder(o.OrderNo).PaymentCurrency != "" {
		t.Fatal("an unconverted expectation was recorded")
	}
}

// An expectation in a currency the gateway no longer collects cannot be
// honoured; the checkout refuses instead of asking for another amount.
func TestCheckoutRejectsARecordedChargeInAnotherCurrency(t *testing.T) {
	f := newPortalFixture(t)
	o := f.pendingOrder("guest-epay", 0, 1000, f.epay(), guestToken("capability"), func(o *order.Order) {
		o.PaymentAmount, o.PaymentCurrency = 150, "USD"
	})
	_, err := f.checkout(context.Background(), o.OrderNo, "capability")
	assertCode(t, err, xerr.OrderStatusError)
}

// A Stripe order has one PaymentIntent: a retried checkout returns its
// client secret instead of creating another the buyer could also pay.
func TestStripeCheckoutReusesTheOrdersIntent(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	f := newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithStripeBackends(fake.Backends)) })
	f.currency = "USD"
	method := f.h.Payment("Stripe", `{"public_key":"pk_test","secret_key":"sk_test","webhook_secret":"whsec_test","payment":"card"}`)
	u, ctx := f.buyer(0, 0)
	o := f.pendingOrder("stripe-order", u.Id, 1990, method)

	first, err := f.checkout(ctx, o.OrderNo, "")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	second, err := f.checkout(ctx, o.OrderNo, "")
	if err != nil {
		t.Fatalf("retried Checkout: %v", err)
	}
	if first.Stripe == nil || second.Stripe == nil || first.Stripe.ClientSecret != second.Stripe.ClientSecret || fake.Created() != 1 {
		t.Fatalf("client secrets %+v / %+v, intents created %d; want one intent", first.Stripe, second.Stripe, fake.Created())
	}
	recorded := f.h.ReloadOrder(o.OrderNo)
	if recorded.TradeNo != "pi_stripe-order" || recorded.PaymentAmount != 1990 || recorded.PaymentCurrency != "USD" {
		t.Fatalf("order = %+v, want the intent and its charge recorded", recorded)
	}
	if key := fake.Keys()[o.OrderNo]; key != "sk_test" {
		t.Fatalf("intent created with key %q, want the method's own key", key)
	}
}
