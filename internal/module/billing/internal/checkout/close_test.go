package checkout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func closeAs(ctx context.Context, svc *Service, orderNo string) error {
	return svc.Close(ctx, &dto.CloseOrderRequest{OrderNo: orderNo})
}

// system is the context of the expiry task and the reconciler.
var system = context.Background()

func (f *checkoutFixture) status(orderNo string) uint8 {
	f.t.Helper()
	return f.h.ReloadOrder(orderNo).Status
}

// markReserved records a plan inventory reservation of orderNo, as the
// purchase flow does, without taking stock.
func (f *checkoutFixture) markReserved(orderNo string) {
	f.t.Helper()
	if err := f.h.Store.Inbox().Insert(context.Background(), subscription.InventoryReserveConsumer, orderNo, ""); err != nil {
		f.t.Fatalf("seed reserve marker: %v", err)
	}
}

// Closing a pending purchase returns everything it held: the plan unit, the
// coupon use and the gift credit, each exactly once.
func TestCloseReturnsWhatThePurchaseHeld(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(400)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	method := f.epay()
	f.h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5 })

	resp, err := f.svc.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "SAVE"})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if f.h.ReloadPlan(plan.Id).Inventory != 2 || f.h.ReloadCoupon("SAVE").UsedCount != 1 || f.h.ReloadWallet(u.Id).GiftAmount != 0 {
		t.Fatal("the purchase did not hold its stock, coupon use and gift credit")
	}
	for range 2 {
		if err := closeAs(ctx, f.svc, resp.OrderNo); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if f.status(resp.OrderNo) != order.StatusClosed {
		t.Fatalf("status = %d, want closed", f.status(resp.OrderNo))
	}
	if inventory := f.h.ReloadPlan(plan.Id).Inventory; inventory != 3 {
		t.Fatalf("inventory = %d, want 3", inventory)
	}
	if used := f.h.ReloadCoupon("SAVE").UsedCount; used != 0 {
		t.Fatalf("coupon uses = %d, want 0", used)
	}
	if gift := f.h.ReloadWallet(u.Id).GiftAmount; gift != 400 {
		t.Fatalf("gift credit = %d, want 400", gift)
	}
	gifts := f.h.GiftLogs(u.Id)
	if len(gifts) != 2 {
		t.Fatalf("gift ledger = %+v, want the deduction and one refund", gifts)
	}
	refund := gifts[1]
	if refund.Remark != ledger.RemarkCancellationRefund || refund.Amount != 400 || refund.Balance != 400 ||
		refund.OrderNo != resp.OrderNo || refund.Timestamp == 0 {
		t.Fatalf("refund entry = %+v", refund)
	}
}

// A purchase whose plan sold out between the preview and the reservation is
// closed again, returning its coupon use and gift credit.
func TestPurchaseOfASoldOutPlanReleasesTheOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(300)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 1 })
	method := f.epay()
	f.h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5 })
	f.svc.deps.Inventory = soldOut{}

	_, err := f.svc.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "SAVE"})
	assertCode(t, err, xerr.SubscribeOutOfStock)
	orders := f.h.Orders(u.Id)
	if len(orders) != 1 || orders[0].Status != order.StatusClosed {
		t.Fatalf("orders = %+v, want the one order closed", orders)
	}
	if f.h.ReloadCoupon("SAVE").UsedCount != 0 || f.h.ReloadWallet(u.Id).GiftAmount != 300 || f.h.ReloadPlan(plan.Id).Inventory != 1 {
		t.Fatal("the sold-out order kept its coupon use, gift credit or stock")
	}
}

// soldOut is the inventory of a plan another buyer just emptied.
type soldOut struct{}

func (soldOut) Reserve(context.Context, string, int64) error { return subscription.ErrOutOfStock }
func (soldOut) Restore(context.Context, string, int64) error { return nil }

// A payment callback can mark the order paid between the close's read and
// its transaction; the close must not turn the paid order back into a
// closed one or release what the paid order holds.
func TestCloseDoesNotOverwriteConcurrentPayment(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(400)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	method := f.epay()
	resp, err := f.svc.Purchase(ctx, &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	f.svc.deps.Tx = raceTransactor{tx: f.h.Store, compete: func() {
		if _, err := f.h.Store.Order().MarkOrderPaid(context.Background(), resp.OrderNo, "trade-1"); err != nil {
			t.Fatal(err)
		}
	}}

	if err := closeAs(system, f.svc, resp.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(resp.OrderNo) != order.StatusPaid {
		t.Fatalf("status = %d, want the concurrent payment kept", f.status(resp.OrderNo))
	}
	if f.h.ReloadWallet(u.Id).GiftAmount != 0 || f.h.ReloadPlan(plan.Id).Inventory != 2 || len(f.h.GiftLogs(u.Id)) != 1 {
		t.Fatal("the paid order's gift credit or stock was released")
	}
}

// Closed guest orders are kept for payment audit: a late provider payment
// must still find its order.
func TestCloseRetainsGuestOrderAndRestoresInventory(t *testing.T) {
	f := newCheckoutFixture(t)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	f.h.Order(&order.Order{OrderNo: "guest-order", Type: order.TypeSubscribe, SubscribeId: plan.Id, Status: order.StatusPending, Amount: 1000})
	if err := f.svc.deps.Inventory.Reserve(context.Background(), "guest-order", plan.Id); err != nil {
		t.Fatal(err)
	}

	if err := closeAs(system, f.svc, "guest-order"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status("guest-order") != order.StatusClosed {
		t.Fatal("the guest order was not kept closed")
	}
	if inventory := f.h.ReloadPlan(plan.Id).Inventory; inventory != 3 {
		t.Fatalf("inventory = %d, want the reserved unit returned", inventory)
	}
}

// Renewals and traffic resets reference a plan but never took stock.
func TestCloseDoesNotRestoreInventoryForRenewalOrTrafficReset(t *testing.T) {
	for _, orderType := range []uint8{order.TypeRenewal, order.TypeResetTraffic} {
		t.Run(fmt.Sprintf("type=%d", orderType), func(t *testing.T) {
			f := newCheckoutFixture(t)
			plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 2 })
			orderNo := fmt.Sprintf("type-%d", orderType)
			f.h.Order(&order.Order{OrderNo: orderNo, Type: orderType, SubscribeId: plan.Id, Status: order.StatusPending})
			f.markReserved(orderNo)

			if err := closeAs(system, f.svc, orderNo); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if f.status(orderNo) != order.StatusClosed || f.h.ReloadPlan(plan.Id).Inventory != 2 {
				t.Fatalf("status = %d inventory = %d, want closed without stock change", f.status(orderNo), f.h.ReloadPlan(plan.Id).Inventory)
			}
		})
	}
}

// A crash between the close commit and the inventory transaction is resumed
// by the retried close, which returns the unit exactly once.
func TestCloseResumesTheInventoryRestorationOfAClosedOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	f.h.Order(&order.Order{OrderNo: "closed-order", Type: order.TypeSubscribe, SubscribeId: plan.Id, Status: order.StatusClosed})
	if err := f.svc.deps.Inventory.Reserve(context.Background(), "closed-order", plan.Id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := closeAs(system, f.svc, "closed-order"); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if inventory := f.h.ReloadPlan(plan.Id).Inventory; inventory != 3 {
		t.Fatalf("inventory = %d, want 3", inventory)
	}
}

// A removed order has nothing to close.
func TestCloseOfAMissingOrderSucceeds(t *testing.T) {
	f := newCheckoutFixture(t)
	if err := closeAs(system, f.svc, "missing"); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
}

// unavailableOrders is an order store whose database connection is gone.
type unavailableOrders struct{}

var _ Orders = unavailableOrders{}

var errConnectionReset = errors.New("connection reset")

func (unavailableOrders) FindOneByOrderNo(context.Context, string) (*order.Order, error) {
	return nil, errConnectionReset
}

func (unavailableOrders) MarkOrderPaid(context.Context, string, string) (bool, error) {
	return false, errConnectionReset
}

func (unavailableOrders) CountUserCouponUsage(context.Context, int64, string) (int64, error) {
	return 0, errConnectionReset
}

func (unavailableOrders) IsUserEligibleForNewOrder(context.Context, int64) (bool, error) {
	return false, errConnectionReset
}

// Only a missing order counts as closed; a failed lookup is an error.
func TestCloseReportsOrderLookupFailures(t *testing.T) {
	f := newCheckoutFixture(t, func(d *Deps) { d.Orders = unavailableOrders{} })
	assertCode(t, closeAs(system, f.svc, "order-1"), xerr.DatabaseQueryError)
}

func TestCloseRejectsAnotherUsersOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	owner, _ := f.buyer(0)
	f.h.Order(&order.Order{OrderNo: "order-1", UserId: owner.Id, Status: order.StatusPending})
	other := user.NewContext(context.Background(), &user.User{Id: owner.Id + 1})

	assertCode(t, closeAs(other, f.svc, "order-1"), xerr.InvalidAccess)
	if f.status("order-1") != order.StatusPending {
		t.Fatal("another user closed the order")
	}
}

// --------------------------------------------------------------- EPay

// epayGateway answers every EPay order query with body.
func epayGateway(t *testing.T, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// unreachableGatewayURL returns a URL on a port that refuses connections
// immediately, so query failures do not wait out the client timeout.
func unreachableGatewayURL() string {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	return server.URL
}

const (
	epayAwaitingPayment = `{"code":1,"msg":"ok","trade_no":"","out_trade_no":"epay-order","type":"alipay","money":"10.00","pid":"1001","status":0}`
	epayPaid            = `{"code":1,"msg":"ok","trade_no":"T-1","out_trade_no":"epay-order","type":"alipay","money":"10.00","pid":"1001","status":1}`
)

// epayOrder seeds buyer's pending EPay order whose checkout started and
// expects ¥10.00, against the gateway at gatewayURL.
func (f *checkoutFixture) epayOrder(gatewayURL string, age time.Duration) (*user.User, *order.Order) {
	f.t.Helper()
	u, _ := f.buyer(0)
	method := f.h.Payment("EPay", fmt.Sprintf(`{"pid":"1001","url":%q,"key":"secret","type":"alipay"}`, gatewayURL))
	o := f.h.Order(&order.Order{
		OrderNo: "epay-order", UserId: u.Id, Status: order.StatusPending, Amount: 1000,
		Method: method.Platform, PaymentId: method.Id, PaymentCurrency: "CNY", PaymentAmount: 1000,
		CreatedAt: time.Now().Add(-age),
	})
	return u, o
}

// The owner gives the order up, which consents to forfeiting a payment the
// gateway cannot confirm; an unpaid or unreachable gateway must not trap it.
func TestCloseEPayOrderUserCancelBypassesUnconfirmedGateway(t *testing.T) {
	for name, gatewayURL := range map[string]string{
		"gateway reports unpaid": epayGateway(t, epayAwaitingPayment),
		"gateway unreachable":    unreachableGatewayURL(),
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, o := f.epayOrder(gatewayURL, time.Minute)
			if err := closeAs(billingtest.UserContext(u), f.svc, o.OrderNo); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if f.status(o.OrderNo) != order.StatusClosed {
				t.Fatal("the owner could not cancel")
			}
		})
	}
}

// The expiry close releases an order the gateway lists as awaiting payment
// once the extended window has passed.
func TestCloseEPayOrderReconcilerClosesUnpaidOrderAfterExtendedWindow(t *testing.T) {
	f := newCheckoutFixture(t)
	_, o := f.epayOrder(epayGateway(t, epayAwaitingPayment), 2*order.PaymentWindow+time.Minute)
	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusClosed {
		t.Fatal("the abandoned order was kept")
	}
}

// A payment whose notification was lost is settled by the expiry close.
func TestCloseEPayOrderSettlesAPaidOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	_, o := f.epayOrder(epayGateway(t, epayPaid), time.Hour)
	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	settled := f.h.ReloadOrder(o.OrderNo)
	if settled.Status != order.StatusPaid || settled.TradeNo != "T-1" || len(f.queue.Activations) != 1 {
		t.Fatalf("order = %+v activations = %v, want settled and activated once", settled, f.queue.Activations)
	}
}

func TestCloseEPayCashierSettlesAPaidOrderWithoutConfiguredType(t *testing.T) {
	f := newCheckoutFixture(t)
	_, o := f.epayOrder(epayGateway(t, epayPaid), time.Hour)
	method, err := f.h.Store.Payment().FindOne(context.Background(), o.PaymentId)
	if err != nil {
		t.Fatal(err)
	}
	method.Config = strings.Replace(method.Config, `"type":"alipay"`, `"type":""`, 1)
	if err := f.h.Store.Payment().Update(context.Background(), method); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Close(context.Background(), &dto.CloseOrderRequest{OrderNo: o.OrderNo}); err != nil {
		t.Fatal(err)
	}
	if got := f.h.ReloadOrder(o.OrderNo); got.Status != order.StatusPaid || got.TradeNo != "T-1" {
		t.Fatalf("cashier order was not settled: status=%d trade=%q", got.Status, got.TradeNo)
	}
}

// Without the gateway's confirmation the reconciler keeps the order pending
// and reports the retryable refusal.
func TestCloseEPayOrderReconcilerKeepsUncertainOrdersPending(t *testing.T) {
	statusOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.php" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"status":"pending"}}`))
	}))
	defer statusOnly.Close()
	old := 2*order.PaymentWindow + time.Minute
	tests := []struct {
		name       string
		gatewayURL string
		age        time.Duration
	}{
		{"awaiting payment inside the extended window", epayGateway(t, epayAwaitingPayment), order.PaymentWindow + time.Minute},
		{"refunded", epayGateway(t, strings.Replace(epayAwaitingPayment, `"status":0`, `"status":2`, 1)), old},
		{"frozen", epayGateway(t, strings.Replace(epayAwaitingPayment, `"status":0`, `"status":3`, 1)), old},
		{"status omitted", epayGateway(t, strings.Replace(epayAwaitingPayment, `,"status":0`, "", 1)), old},
		{"status-only answer", statusOnly.URL, old},
		{"lookup failed", epayGateway(t, `{"code":-1,"msg":"order not found"}`), old},
		{"gateway unreachable", unreachableGatewayURL(), old},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			_, o := f.epayOrder(tt.gatewayURL, tt.age)
			err := closeAs(system, f.svc, o.OrderNo)
			assertUnconfirmed(t, err)
			if f.status(o.OrderNo) != order.StatusPending {
				t.Fatal("an unconfirmed order was closed")
			}
		})
	}
}

// assertUnconfirmed checks the refusal to close an order the gateway could
// not confirm: a retryable business conflict schedulers recognise.
func assertUnconfirmed(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrGatewayUnconfirmed) {
		t.Fatalf("error = %v, want ErrGatewayUnconfirmed", err)
	}
	assertCode(t, err, xerr.PaymentStatusUnconfirmed)
}

// An administrator resolves an order by hand: the owner check does not
// apply, and like the owner the administrator may forfeit an unconfirmed
// payment.
func TestCloseByAdminClosesUnconfirmedEPayOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	_, o := f.epayOrder(unreachableGatewayURL(), time.Hour)
	admin := user.NewContext(context.Background(), &user.User{Id: 99})

	assertCode(t, closeAs(admin, f.svc, o.OrderNo), xerr.InvalidAccess)
	closed, err := f.svc.CloseByAdmin(admin, o.OrderNo, 99)
	if err != nil || !closed || f.status(o.OrderNo) != order.StatusClosed {
		t.Fatalf("CloseByAdmin = (%t, %v), want the order closed", closed, err)
	}
	if closed, err = f.svc.CloseByAdmin(admin, o.OrderNo, 99); err != nil || closed {
		t.Fatalf("repeated CloseByAdmin = (%t, %v), want (false, nil)", closed, err)
	}
}

// ------------------------------------------------------------- Alipay

func alipayPaid(amount string) billingtest.AlipayAnswer {
	return billingtest.AlipayAnswer{Signed: true, Biz: `{"code":"10000","msg":"Success","trade_no":"2026080222001430000000000001","out_trade_no":"alipay-order","trade_status":"TRADE_SUCCESS","total_amount":"` + amount + `"}`}
}

var (
	alipayWaiting  = billingtest.AlipayAnswer{Signed: true, Biz: `{"code":"10000","msg":"Success","trade_no":"2026080222001430000000000001","out_trade_no":"alipay-order","trade_status":"WAIT_BUYER_PAY","total_amount":"10.00"}`}
	alipayNotExist = billingtest.AlipayAnswer{Biz: `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"trade not exist"}`}
	alipayClosedOK = billingtest.AlipayAnswer{Signed: true, Biz: `{"code":"10000","msg":"Success","out_trade_no":"alipay-order","trade_no":"2026080222001430000000000001"}`}
	alipayCloseErr = billingtest.AlipayAnswer{Biz: `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_STATUS_ERROR","sub_msg":"trade status error"}`}
)

// alipayOrder seeds buyer's pending ¥10.00 face-to-face order whose QR code
// was issued, against the gateway at gatewayURL.
func (f *checkoutFixture) alipayOrder(gatewayURL string, adjust ...func(*order.Order)) (*user.User, *order.Order) {
	f.t.Helper()
	u, _ := f.buyer(0)
	method := f.h.Payment("AlipayF2F", billingtest.AlipayConfig(f.t, "2021000000000000", gatewayURL))
	o := &order.Order{
		OrderNo: "alipay-order", UserId: u.Id, Status: order.StatusPending, Amount: 1000,
		Method: method.Platform, PaymentId: method.Id, PaymentCurrency: "CNY", PaymentAmount: 1000,
	}
	for _, fn := range adjust {
		fn(o)
	}
	return u, f.h.Order(o)
}

// aged dates an order age ago.
func aged(age time.Duration) func(*order.Order) {
	return func(o *order.Order) { o.CreatedAt = time.Now().Add(-age) }
}

// A paid trade whose notification never arrived was once silently
// cancelled by the expiry close. Closing must ask the gateway first and
// settle a trade it reports as paid.
func TestCloseAlipayOrderSettlesPaidTradeWhenCallbackWasLost(t *testing.T) {
	f := newCheckoutFixture(t)
	gw := billingtest.NewFakeAlipay(t, func(method string, _ int, _ map[string]any) billingtest.AlipayAnswer {
		if method != "alipay.trade.query" {
			t.Errorf("unexpected gateway call %q", method)
		}
		return alipayPaid("10.00")
	})
	_, o := f.alipayOrder(gw.URL)

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	settled := f.h.ReloadOrder(o.OrderNo)
	if settled.Status != order.StatusPaid || settled.TradeNo != "2026080222001430000000000001" {
		t.Fatalf("order = %+v, want settled with the gateway trade", settled)
	}
	if len(f.queue.Activations) != 1 || gw.Calls("alipay.trade.close") != 0 {
		t.Fatalf("activations = %v closes = %d, want one activation and no gateway close", f.queue.Activations, gw.Calls("alipay.trade.close"))
	}
}

// A paid trade that does not match the recorded expectation neither settles
// nor closes; it stays pending for manual resolution.
func TestCloseAlipayOrderRejectsMismatchedPaidTrade(t *testing.T) {
	f := newCheckoutFixture(t)
	gw := billingtest.NewFakeAlipay(t, func(string, int, map[string]any) billingtest.AlipayAnswer { return alipayPaid("9.00") })
	_, o := f.alipayOrder(gw.URL)

	if err := closeAs(system, f.svc, o.OrderNo); err == nil {
		t.Fatal("Close accepted a paid trade with a mismatched amount")
	}
	if f.status(o.OrderNo) != order.StatusPending || len(f.queue.Activations) != 0 {
		t.Fatal("the mismatched trade changed the order")
	}
}

// A face-to-face trade exists only once the buyer scans the QR code, so a
// missing trade proves no money was collected so far. The QR code stays
// scannable until the trade expiry set at checkout, the end of the order's
// payment window, so the expiry close waits until the order is
// order.UnpaidCloseAge old before it releases what the order holds; the
// owner may give it up at once.
func TestCloseAlipayOrderClosesWhenQRWasNeverScanned(t *testing.T) {
	t.Run("expiry close inside the extended window keeps the order", func(t *testing.T) {
		f := newCheckoutFixture(t)
		gw := billingtest.NewFakeAlipay(t, func(string, int, map[string]any) billingtest.AlipayAnswer { return alipayNotExist })
		_, o := f.alipayOrder(gw.URL, aged(order.PaymentWindow+time.Minute))

		assertUnconfirmed(t, closeAs(system, f.svc, o.OrderNo))
		if f.status(o.OrderNo) != order.StatusPending || gw.Calls("alipay.trade.close") != 0 {
			t.Fatal("want the order kept pending without a gateway close")
		}
	})
	t.Run("expiry close after the extended window closes", func(t *testing.T) {
		f := newCheckoutFixture(t)
		gw := billingtest.NewFakeAlipay(t, func(string, int, map[string]any) billingtest.AlipayAnswer { return alipayNotExist })
		_, o := f.alipayOrder(gw.URL, aged(order.UnpaidCloseAge+time.Minute))

		if err := closeAs(system, f.svc, o.OrderNo); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if f.status(o.OrderNo) != order.StatusClosed || gw.Calls("alipay.trade.close") != 0 {
			t.Fatal("want the order closed without a gateway close")
		}
	})
	t.Run("owner closes at once", func(t *testing.T) {
		f := newCheckoutFixture(t)
		gw := billingtest.NewFakeAlipay(t, func(string, int, map[string]any) billingtest.AlipayAnswer { return alipayNotExist })
		u, o := f.alipayOrder(gw.URL)

		if err := closeAs(billingtest.UserContext(u), f.svc, o.OrderNo); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if f.status(o.OrderNo) != order.StatusClosed || gw.Calls("alipay.trade.close") != 0 {
			t.Fatal("want the order closed without a gateway close")
		}
	})
}

// ------------------------------------------- checkout racing the close

// A checkout records the payment expectation before it creates the payment
// at the gateway. A close that read the order before that write sees a
// checkout that "never started" and would close without asking the gateway,
// leaving an Alipay QR code, an EPay payment page or a Stripe client secret
// payable on a closed order; every gateway therefore requires the checkout
// to be unchanged when the close commits. The gateways are unreachable: the
// close must not contact them either way.
func TestCloseRechecksConcurrentCheckoutForEveryGateway(t *testing.T) {
	unreachable := unreachableGatewayURL()
	methods := map[string]func(f *checkoutFixture) *payment.Payment{
		"EPay": func(f *checkoutFixture) *payment.Payment {
			return f.h.Payment("EPay", fmt.Sprintf(`{"pid":"1001","url":%q,"key":"secret","type":"alipay"}`, unreachable))
		},
		"AlipayF2F": func(f *checkoutFixture) *payment.Payment {
			return f.h.Payment("AlipayF2F", billingtest.AlipayConfig(f.t, "2021000000000000", unreachable))
		},
		"Stripe": func(f *checkoutFixture) *payment.Payment {
			return f.h.Payment("Stripe", `{"public_key":"pk_test","secret_key":"sk_test","webhook_secret":"whsec_test","payment":"card"}`)
		},
	}
	for platform, seedMethod := range methods {
		t.Run(platform+"/checkout starts during the close", func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, _ := f.buyer(40)
			method := seedMethod(f)
			o := f.h.Order(&order.Order{
				OrderNo: "racing-order", UserId: u.Id, Status: order.StatusPending, Type: order.TypeSubscribe, Amount: 1000, GiftAmount: 40,
				Method: method.Platform, PaymentId: method.Id, CreatedAt: time.Now().Add(-order.PaymentWindow - time.Minute),
			})
			f.svc.deps.Tx = raceTransactor{tx: f.h.Store, compete: func() {
				if err := f.h.DB.Model(&order.Order{}).Where("order_no = ?", o.OrderNo).Update("payment_currency", "CNY").Error; err != nil {
					t.Fatal(err)
				}
			}}

			assertUnconfirmed(t, closeAs(system, f.svc, o.OrderNo))
			if f.status(o.OrderNo) != order.StatusPending || f.h.ReloadWallet(u.Id).GiftAmount != 40 || len(f.h.GiftLogs(u.Id)) != 0 {
				t.Fatal("an order whose checkout just started was closed or its gift credit released")
			}
		})
		t.Run(platform+"/no checkout closes", func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, _ := f.buyer(40)
			method := seedMethod(f)
			o := f.h.Order(&order.Order{
				OrderNo: "idle-order", UserId: u.Id, Status: order.StatusPending, Type: order.TypeSubscribe, Amount: 1000, GiftAmount: 40,
				Method: method.Platform, PaymentId: method.Id, CreatedAt: time.Now().Add(-order.PaymentWindow - time.Minute),
			})

			if err := closeAs(system, f.svc, o.OrderNo); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if f.status(o.OrderNo) != order.StatusClosed || f.h.ReloadWallet(u.Id).GiftAmount != 80 {
				t.Fatal("an order before checkout should close and return its gift credit")
			}
		})
	}
}

// A scanned but unpaid trade keeps a payable QR code alive, so it is voided
// at the gateway before the local close.
func TestCloseAlipayOrderVoidsScannedUnpaidTradeAtGateway(t *testing.T) {
	f := newCheckoutFixture(t)
	gw := billingtest.NewFakeAlipay(t, func(method string, _ int, _ map[string]any) billingtest.AlipayAnswer {
		if method == "alipay.trade.close" {
			return alipayClosedOK
		}
		return alipayWaiting
	})
	_, o := f.alipayOrder(gw.URL)

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusClosed || gw.Calls("alipay.trade.close") != 1 {
		t.Fatalf("status = %d gateway closes = %d, want closed after one gateway close", f.status(o.OrderNo), gw.Calls("alipay.trade.close"))
	}
}

// A payment can land between the query and the gateway close; the refused
// close triggers one requery and the paid trade settles.
func TestCloseAlipayOrderSettlesPaymentThatRacesGatewayClose(t *testing.T) {
	f := newCheckoutFixture(t)
	gw := billingtest.NewFakeAlipay(t, func(method string, call int, _ map[string]any) billingtest.AlipayAnswer {
		switch {
		case method == "alipay.trade.close":
			return alipayCloseErr
		case call == 1:
			return alipayWaiting
		default:
			return alipayPaid("10.00")
		}
	})
	_, o := f.alipayOrder(gw.URL)

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusPaid || len(f.queue.Activations) != 1 {
		t.Fatalf("status = %d activations = %v, want settled once", f.status(o.OrderNo), f.queue.Activations)
	}
}

// Without gateway confirmation the expiry close keeps the order pending,
// while the owner may still give it up.
func TestCloseAlipayOrderWithoutGatewayConfirmation(t *testing.T) {
	f := newCheckoutFixture(t)
	u, o := f.alipayOrder(unreachableGatewayURL())

	assertUnconfirmed(t, closeAs(system, f.svc, o.OrderNo))
	if f.status(o.OrderNo) != order.StatusPending {
		t.Fatal("the reconciler closed an unconfirmed order")
	}
	if err := closeAs(billingtest.UserContext(u), f.svc, o.OrderNo); err != nil {
		t.Fatalf("owner Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusClosed {
		t.Fatal("the owner could not cancel")
	}
}

// ------------------------------------------------------------- Stripe

// stripeOrder seeds buyer's pending order of ¥10.00 whose checkout charges
// the intent pi_1 $1.50: the stored expectation differs from the order
// amount in the site currency.
func (f *checkoutFixture) stripeOrder(fake *billingtest.FakeStripe, intentAmount int64, intentCurrency, status string, adjust ...func(*order.Order)) *order.Order {
	f.t.Helper()
	u, _ := f.buyer(0)
	method := f.h.Payment("Stripe", `{"public_key":"pk_test","secret_key":"sk_test","webhook_secret":"whsec_test","payment":"card"}`)
	fake.Seed("pi_1", intentAmount, intentCurrency, status, "stripe-order", "card")
	o := &order.Order{
		OrderNo: "stripe-order", UserId: u.Id, Status: order.StatusPending, Amount: 1000,
		Method: method.Platform, PaymentId: method.Id, PaymentCurrency: "USD", PaymentAmount: 150, TradeNo: "pi_1",
	}
	for _, fn := range adjust {
		fn(o)
	}
	return f.h.Order(o)
}

func stripeFixture(t *testing.T) (*checkoutFixture, *billingtest.FakeStripe) {
	fake := billingtest.NewFakeStripe(t)
	return newCheckoutFixture(t, withGateways(gateway.NewRegistry(gateway.WithStripeBackends(fake.Backends)))), fake
}

// The intent is verified against the charge recorded at checkout, not the
// order amount in the site currency.
func TestCloseStripeOrderSettlesTheRecordedCharge(t *testing.T) {
	f, fake := stripeFixture(t)
	o := f.stripeOrder(fake, 150, "usd", "succeeded")

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusPaid || len(f.queue.Activations) != 1 || len(fake.Canceled()) != 0 {
		t.Fatalf("status = %d activations = %v canceled = %v, want the payment settled", f.status(o.OrderNo), f.queue.Activations, fake.Canceled())
	}
}

// An unpaid intent is cancelled so its client secret cannot be paid after
// the local close.
func TestCloseStripeOrderCancelsAnUnpaidIntent(t *testing.T) {
	f, fake := stripeFixture(t)
	o := f.stripeOrder(fake, 150, "usd", "requires_payment_method")

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusClosed || fake.Status("pi_1") != "canceled" {
		t.Fatalf("status = %d intent = %s, want closed and cancelled", f.status(o.OrderNo), fake.Status("pi_1"))
	}
}

// An intent that does not match the recorded charge is neither settled nor
// cancelled.
func TestCloseStripeOrderRejectsAMismatchedIntent(t *testing.T) {
	f, fake := stripeFixture(t)
	o := f.stripeOrder(fake, 1000, "cny", "succeeded")

	if err := closeAs(system, f.svc, o.OrderNo); err == nil {
		t.Fatal("Close accepted an intent of another charge")
	}
	if f.status(o.OrderNo) != order.StatusPending || len(f.queue.Activations) != 0 {
		t.Fatal("the mismatched intent changed the order")
	}
}

// Intents created before expectations were recorded charged the order amount
// in the site currency.
func TestCloseStripeOrderWithoutRecordedChargeUsesTheSiteCurrency(t *testing.T) {
	f, fake := stripeFixture(t)
	o := f.stripeOrder(fake, 1000, "cny", "succeeded", func(o *order.Order) { o.PaymentCurrency, o.PaymentAmount = "", 0 })

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusPaid {
		t.Fatalf("status = %d, want the legacy intent settled", f.status(o.OrderNo))
	}
}

// An intent Stripe already canceled can never be paid. The expiry close
// takes it as canceled instead of asking Stripe to cancel it again, which
// Stripe refuses; that refusal used to keep the order pending forever.
func TestCloseStripeOrderClosesAnAlreadyCanceledIntent(t *testing.T) {
	f, fake := stripeFixture(t)
	o := f.stripeOrder(fake, 150, "usd", "canceled")

	if err := closeAs(system, f.svc, o.OrderNo); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.status(o.OrderNo) != order.StatusClosed || len(fake.Canceled()) != 0 || len(f.queue.Activations) != 0 {
		t.Fatalf("status = %d canceled = %v activations = %v, want the order closed without another cancellation", f.status(o.OrderNo), fake.Canceled(), f.queue.Activations)
	}
}
