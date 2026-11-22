package v2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// Design: one idempotency key yields one order and one reservation; a
// different request body under the key is refused.
func TestCreateAndCheckoutIsIdempotent(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	u, ctx := f.buyer(0, 0)
	plan, method := f.plan(5), f.epay()

	first, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000001")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	retry, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000001")
	if err != nil {
		t.Fatalf("retried CreateAndCheckout: %v", err)
	}
	if retry.Order.OrderNo != first.Order.OrderNo || retry.Payment == nil || retry.Payment.CheckoutURL != first.Payment.CheckoutURL {
		t.Fatalf("retry = %+v, want the first order and payment", retry)
	}
	changed := purchase(plan, method)
	changed.Quantity = 2
	if _, err := f.svc.CreateAndCheckout(ctx, changed, "key-0000000001"); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("a different body under the key: %v, want ErrIdempotencyKeyReused", err)
	}
	// The key is bound to its user: another user's identical body is a
	// different request.
	_, other := f.buyer(0, 0)
	if _, err := f.svc.CreateAndCheckout(other, purchase(plan, method), "key-0000000001"); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("another user's request under the key: %v, want ErrIdempotencyKeyReused", err)
	}
	if orders := f.h.Orders(u.Id); len(orders) != 1 || f.h.ReloadPlan(plan.Id).Inventory != 4 {
		t.Fatalf("orders = %d inventory = %d, want one order holding one unit", len(orders), f.h.ReloadPlan(plan.Id).Inventory)
	}
}

// staleIdempotencyLookup misses the order a concurrent request with the same
// key committed, as the initial lookup of a request racing it does; every
// other read goes to orders.
type staleIdempotencyLookup struct {
	orders Orders
	misses atomic.Int32
}

var _ Orders = (*staleIdempotencyLookup)(nil)

func (s *staleIdempotencyLookup) FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error) {
	return s.orders.FindOneByOrderNo(ctx, orderNo)
}

func (s *staleIdempotencyLookup) FindOneByIdempotencyKey(ctx context.Context, key string) (*order.Order, error) {
	if s.misses.Add(-1) >= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return s.orders.FindOneByIdempotencyKey(ctx, key)
}

// Design: concurrent submissions of one key produce one order and one
// reservation. The loser's creation fails on the unique key, rolls back what
// it held and answers with the winner's order.
func TestConcurrentCreateWithOneKeyReservesOnce(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	// The first order spends 1000 of the gift credit; the racing attempt
	// spends the rest until its transaction rolls back.
	u, ctx := f.buyer(0, 1300)
	plan, method := f.plan(5), f.epay()
	first, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000002")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	lookup := &staleIdempotencyLookup{orders: f.svc.deps.Orders}
	lookup.misses.Store(1)
	f.svc.deps.Orders = lookup

	racing, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000002")
	if err != nil {
		t.Fatalf("racing CreateAndCheckout: %v", err)
	}
	if racing.Order.OrderNo != first.Order.OrderNo {
		t.Fatalf("racing request answered order %s, want %s", racing.Order.OrderNo, first.Order.OrderNo)
	}
	if orders := f.h.Orders(u.Id); len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	if f.h.ReloadPlan(plan.Id).Inventory != 4 || f.h.ReloadWallet(u.Id).GiftAmount != 300 || len(f.h.GiftLogs(u.Id)) != 1 {
		t.Fatal("the losing request kept a reservation")
	}
}

// Design: when the gateway payload cannot be created, a retry returns the
// same order and the same payment snapshot, even after the rate moved.
func TestGatewayFailureRetryReturnsTheSameOrderAndCharge(t *testing.T) {
	gw := billingtest.NewFakeAlipay(t, func(method string, call int, biz map[string]any) billingtest.AlipayAnswer {
		if call == 1 {
			return billingtest.AlipayAnswer{Biz: `{"code":"20000","msg":"Service Currently Unavailable","sub_code":"isp.unknow-error"}`}
		}
		return billingtest.AlipayAnswer{Signed: true, Biz: fmt.Sprintf(
			`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.alipay.com/retry"}`, biz["out_trade_no"])}
	})
	f := newV2Fixture(t, v2Options{})
	f.currency, f.rates.rate = "USD", 7.1
	_, ctx := f.buyer(0, 0)
	plan := f.plan(5)
	method := f.h.Payment("AlipayF2F", billingtest.AlipayConfig(t, "2021000000000000", gw.URL))

	if _, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000003"); err == nil {
		t.Fatal("the gateway failure was not reported")
	}
	firstAmount := gw.LastBiz("alipay.trade.precreate")["total_amount"]
	f.rates.Set(7.5)
	retry, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000003")
	if err != nil {
		t.Fatalf("retried CreateAndCheckout: %v", err)
	}
	if retry.Payment == nil || retry.Payment.CheckoutURL != "https://qr.alipay.com/retry" {
		t.Fatalf("payment = %+v, want the QR code", retry.Payment)
	}
	if again := gw.LastBiz("alipay.trade.precreate")["total_amount"]; firstAmount != "71.00" || again != firstAmount {
		t.Fatalf("gateway asked %v then %v, want 71.00 both times", firstAmount, again)
	}
	if o := f.h.ReloadOrder(retry.Order.OrderNo); o.PaymentAmount != 7100 || o.PaymentCurrency != "CNY" {
		t.Fatalf("recorded charge = %d %s, want 7100 CNY", o.PaymentAmount, o.PaymentCurrency)
	}
}

const stripeConfig = `{"public_key":"pk_test","secret_key":"sk_test","webhook_secret":"whsec_test","payment":"card"}`

// Design: a Stripe retry returns the order's one PaymentIntent.
func TestStripeRetryReturnsTheOnePaymentIntent(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	f := newV2Fixture(t, v2Options{gateways: gateway.NewRegistry(gateway.WithStripeBackends(fake.Backends))})
	_, ctx := f.buyer(0, 0)
	plan, method := f.plan(5), f.h.Payment("Stripe", stripeConfig)

	first, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000004")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	resumed, err := f.svc.Checkout(ctx, first.Order.OrderNo, &dto.V2CheckoutOrderRequest{})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if first.Payment.Stripe == nil || resumed.Payment.Stripe == nil || first.Payment.Stripe.ClientSecret != resumed.Payment.Stripe.ClientSecret || fake.Created() != 1 {
		t.Fatalf("client secrets %+v / %+v with %d intents, want one intent", first.Payment.Stripe, resumed.Payment.Stripe, fake.Created())
	}
}

// competingClaim lets another checkout claim its intent for the order just
// before this one claims its own; everything else goes to orders.
type competingClaim struct {
	orders portal.Orders
	claim  func(orderNo string)
}

var _ portal.Orders = competingClaim{}

func (c competingClaim) FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error) {
	return c.orders.FindOneByOrderNo(ctx, orderNo)
}

func (c competingClaim) CountPendingGuestOrders(ctx context.Context, authType, identifier string, since time.Time) (int64, error) {
	return c.orders.CountPendingGuestOrders(ctx, authType, identifier, since)
}

func (c competingClaim) UpdatePaymentExpectation(ctx context.Context, orderNo string, amount int64, currency string) (bool, error) {
	return c.orders.UpdatePaymentExpectation(ctx, orderNo, amount, currency)
}

func (c competingClaim) SetPaymentTradeNoIfEmpty(ctx context.Context, orderNo, tradeNo string) (bool, error) {
	c.claim(orderNo)
	return c.orders.SetPaymentTradeNoIfEmpty(ctx, orderNo, tradeNo)
}

func (c competingClaim) BeginPaymentSession(ctx context.Context, orderNo string) (bool, error) {
	return c.orders.BeginPaymentSession(ctx, orderNo)
}

func (c competingClaim) CompletePaymentSession(ctx context.Context, orderNo, tradeNo, session string) (bool, error) {
	return c.orders.CompletePaymentSession(ctx, orderNo, tradeNo, session)
}

func (c competingClaim) UpdateOrderStatusFrom(ctx context.Context, orderNo string, from, status uint8) (bool, error) {
	return c.orders.UpdateOrderStatusFrom(ctx, orderNo, from, status)
}

// Design: two checkouts can each create an intent; the one that loses the
// claim cancels its own and answers with the winner's.
func TestStripeCheckoutsRacingForTheOrderKeepOneIntent(t *testing.T) {
	fake := billingtest.NewFakeStripe(t)
	var f *v2Fixture
	f = newV2Fixture(t, v2Options{
		gateways: gateway.NewRegistry(gateway.WithStripeBackends(fake.Backends)),
		orders: func(orders portal.Orders) portal.Orders {
			return competingClaim{orders: orders, claim: func(orderNo string) {
				fake.Seed("pi_winner", 1000, "cny", "requires_payment_method", orderNo, "card")
				if _, err := orders.SetPaymentTradeNoIfEmpty(context.Background(), orderNo, "pi_winner"); err != nil {
					f.t.Error(err)
				}
			}}
		},
	})
	_, ctx := f.buyer(0, 0)
	plan, method := f.plan(5), f.h.Payment("Stripe", stripeConfig)

	resp, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000005")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	if resp.Payment.Stripe == nil || resp.Payment.Stripe.ClientSecret != "pi_winner_secret" {
		t.Fatalf("payment = %+v, want the winner's intent", resp.Payment.Stripe)
	}
	if canceled := fake.Canceled(); len(canceled) != 1 || canceled[0] == "pi_winner" {
		t.Fatalf("canceled intents = %v, want only the loser's", canceled)
	}
	if o := f.h.ReloadOrder(resp.Order.OrderNo); o.TradeNo != "pi_winner" {
		t.Fatalf("order intent = %q, want pi_winner", o.TradeNo)
	}
}

// Design: a balance payment is never debited twice.
func TestBalancePaymentIsNeverDebitedTwice(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	u, ctx := f.buyer(5000, 0)
	plan, method := f.plan(5), f.h.Payment("balance", "")

	first, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000006")
	if err != nil || first.Payment == nil || first.Payment.Type != "balance" || first.Order.PaymentStatus != order.PaymentStatusName(order.StatusPaid) {
		t.Fatalf("CreateAndCheckout = (%+v, %v), want a paid balance order", first, err)
	}
	retry, err := f.svc.CreateAndCheckout(ctx, purchase(plan, method), "key-0000000006")
	if err != nil || retry.Payment != nil || retry.Order.OrderNo != first.Order.OrderNo {
		t.Fatalf("retry = (%+v, %v), want the paid order without another payment", retry, err)
	}
	_, err = f.svc.Checkout(ctx, first.Order.OrderNo, &dto.V2CheckoutOrderRequest{})
	assertCode(t, err, xerr.OrderStatusError)
	if w := f.h.ReloadWallet(u.Id); w.Balance != 4000 || len(f.h.BalanceLogs(u.Id)) != 1 {
		t.Fatalf("wallet = %+v, want one debit", w)
	}
}

func eventNames(events []order.Event) []string {
	names := make([]string, len(events))
	for i, event := range events {
		names[i] = event.EventType
	}
	return names
}

// Design: a payment callback, the expiry close and activation racing for
// one order leave a final state whose events tell the same story.
func TestCallbackCloseAndActivationRaceToOneConsistentOutcome(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	u, ctx := f.buyer(0, 0)
	plan, method := f.plan(100), f.epay()
	orders := f.h.Store.Order()
	for i := range 12 {
		resp, err := f.checkout.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id})
		if err != nil {
			t.Fatalf("Purchase: %v", err)
		}
		orderNo := resp.OrderNo
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := f.checkout.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: orderNo}); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			pending, err := orders.FindOneByOrderNo(context.Background(), orderNo)
			if err != nil {
				t.Error(err)
				return
			}
			// A callback losing to the close is refused, never applied.
			_ = settle.VerifiedPayment(context.Background(), orders, f.queue, pending, fmt.Sprintf("trade-%d", i))
		}()
		wg.Wait()
		// Activation finishes a paid order and leaves a closed one alone.
		if _, err := orders.UpdateOrderStatusFrom(context.Background(), orderNo, order.StatusPaid, order.StatusFinished); err != nil {
			t.Fatal(err)
		}
		final := f.h.ReloadOrder(orderNo)
		names := eventNames(f.h.Events(orderNo))
		switch final.Status {
		case order.StatusFinished:
			if !slices.Equal(names, []string{"order.created", "order.payment_paid", "order.fulfilled"}) {
				t.Fatalf("finished order events = %v", names)
			}
		case order.StatusClosed:
			if !slices.Equal(names, []string{"order.created", "order.closed"}) {
				t.Fatalf("closed order events = %v", names)
			}
		default:
			t.Fatalf("final status = %d", final.Status)
		}
		if final.StateVersion != int64(len(names)) {
			t.Fatalf("state version = %d after %d events", final.StateVersion, len(names))
		}
	}
	if len(f.h.Orders(u.Id)) != 12 {
		t.Fatal("unexpected orders")
	}
}

// Design: a guest replay under its key proves the password against the
// order's password hash, since the stored request hash carries none; a
// different password is refused like a changed body, and the stored hash
// reveals nothing about the password.
func TestGuestReplayProvesThePasswordAgainstTheOrder(t *testing.T) {
	f := newV2Fixture(t, v2Options{})
	ctx := context.Background()
	plan, method := f.plan(5), f.epay()

	first, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000007")
	if err != nil {
		t.Fatalf("CreateAndCheckout: %v", err)
	}
	retry, err := f.svc.CreateAndCheckout(ctx, guestPurchase(plan, method), "key-0000000007")
	if err != nil || retry.Order.OrderNo != first.Order.OrderNo {
		t.Fatalf("retry = (%+v, %v), want the first order", retry, err)
	}
	wrong := guestPurchase(plan, method)
	wrong.Guest.Password = "another-password"
	if _, err := f.svc.CreateAndCheckout(ctx, wrong, "key-0000000007"); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("replay with another password: %v, want ErrIdempotencyKeyReused", err)
	}
	stored := f.h.ReloadOrder(first.Order.OrderNo)
	if other, _ := requestHash(ctx, wrong); stored.IdempotencyHash != other {
		t.Fatal("the stored request hash distinguishes passwords")
	}
	if guests := f.h.Orders(0); len(guests) != 1 {
		t.Fatalf("guest orders = %d, want one", len(guests))
	}
}
