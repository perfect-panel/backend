package billing_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// facade is the billing module assembled like the application assembles it,
// over the real repositories.
type facade struct {
	svc   billing.Service
	h     *billingtest.Harness
	queue *billingtest.Queue
}

func newFacade(t *testing.T) *facade {
	t.Helper()
	h := billingtest.New(t)
	queue := &billingtest.Queue{}
	svc := billing.New(billing.Deps{
		Orders: h.Store.Order(), OrderEvents: h.Store.OrderEvent(), Payments: h.Store.Payment(), Coupons: h.Store.Coupon(),
		Withdrawals: h.Store.UserWithdrawal(), Plans: h.Store.Subscribe(), UserSubs: h.Store.UserSubscription(),
		Store: h.Store, Inventory: subscription.NewInventory(h.Store), Tx: h.Store, Queue: queue, Redis: h.Redis,
		SingleModel: func() bool { return false }, CurrencyUnit: func() string { return "CNY" },
		Logs: h.Store.Log(), UserCache: &billingtest.UserCache{}, Affiliates: h.Store.User(), AuthMethods: h.Store.UserAuth(),
		UserProfiles: h.Store.User(), InvitePolicy: func() (uint8, bool) { return 0, false },
		PortalPlans: h.Store.Subscribe(), GuestAccounts: h.Store.UserAuth(), Sessions: h.Redis, GuestCheckoutCache: h.Redis,
		ExchangeRate: billing.NewCurrencyRateCache(0),
		Portal: billing.PortalConfig{
			SiteHost: func() string { return "panel.example.com" }, CurrencyUnit: func() string { return "CNY" }, JwtSecret: "secret", JwtExpire: 3600,
		},
	})
	return &facade{svc: svc, h: h, queue: queue}
}

const epayConfig = `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`

// adminContext is an administrator who owns none of the orders.
var adminContext = user.NewContext(context.Background(), &user.User{Id: 9999})

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

func TestUpdateOrderStatusRejectsInvalidTransitions(t *testing.T) {
	f := newFacade(t)
	o := f.h.Order(&order.Order{OrderNo: "o-1", Status: order.StatusPending})
	for _, tt := range []struct {
		req  *dto.UpdateOrderStatusRequest
		code uint32
	}{
		{&dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusFinished, TradeNo: "t"}, xerr.InvalidOrderTransition},
		{&dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPending, TradeNo: "t"}, xerr.InvalidOrderTransition},
		{&dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPaid}, xerr.TradeNoRequired},
		{&dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusClosed, TradeNo: "t"}, xerr.InvalidOrderCloseRequest},
		{&dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusClosed, PaymentId: 9}, xerr.InvalidOrderCloseRequest},
		{&dto.UpdateOrderStatusRequest{Id: o.Id + 100, Status: order.StatusClosed}, xerr.OrderNotExist},
	} {
		assertCode(t, f.svc.UpdateOrderStatus(adminContext, tt.req), tt.code)
	}
	if f.h.ReloadOrder("o-1").Status != order.StatusPending || len(f.queue.Activations) != 0 {
		t.Fatal("a rejected transition changed the order")
	}
}

func TestUpdateOrderStatusMarksPaidAndEnqueuesActivation(t *testing.T) {
	f := newFacade(t)
	o := f.h.Order(&order.Order{OrderNo: "o-2", Status: order.StatusPending})

	if err := f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPaid, TradeNo: "trade-1"}); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if paid := f.h.ReloadOrder("o-2"); paid.Status != order.StatusPaid || paid.TradeNo != "trade-1" {
		t.Fatalf("order = %+v, want paid with the trade number", paid)
	}
	if len(f.queue.Activations) != 1 || f.queue.Activations[0] != "o-2" {
		t.Fatalf("activations = %v", f.queue.Activations)
	}
	assertCode(t, f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPaid, TradeNo: "trade-2"}), xerr.OrderStatusError)
}

// Marking an order paid commits the order; a queue that is down only delays
// the activation, which the paid-order reconciler re-drives.
func TestUpdateOrderStatusReportsThePaidOrderWhenTheQueueIsDown(t *testing.T) {
	f := newFacade(t)
	f.queue.ActivationErr = errors.New("queue unavailable")
	o := f.h.Order(&order.Order{OrderNo: "o-3", Status: order.StatusPending})

	if err := f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusPaid, TradeNo: "trade-3"}); err != nil {
		t.Fatalf("UpdateOrderStatus = %v, want the committed outcome", err)
	}
	if paid := f.h.ReloadOrder("o-3"); paid.Status != order.StatusPaid {
		t.Fatalf("order = %+v, want paid", paid)
	}
}

// An administrator's close runs the shared close flow: the bare status
// update it replaced kept the coupon use, the gift deduction and the stock.
func TestUpdateOrderStatusCloseReleasesReservations(t *testing.T) {
	f := newFacade(t)
	buyer := f.h.User()
	f.h.Wallet(buyer.Id, 0, 400)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 })
	method := f.h.Payment("EPay", epayConfig)
	f.h.Coupon("SPRING", func(c *coupon.Coupon) { c.Count = 5 })
	resp, err := f.svc.Purchase(billingtest.UserContext(buyer), &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "SPRING"})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	o := f.h.ReloadOrder(resp.OrderNo)

	if err := f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusClosed}); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if f.h.ReloadOrder(o.OrderNo).Status != order.StatusClosed || len(f.queue.Activations) != 0 {
		t.Fatal("the order was not closed")
	}
	if f.h.ReloadCoupon("SPRING").UsedCount != 0 || f.h.ReloadWallet(buyer.Id).GiftAmount != 400 || f.h.ReloadPlan(plan.Id).Inventory != 3 {
		t.Fatal("the close kept the coupon use, gift credit or stock")
	}
	assertCode(t, f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusClosed}), xerr.OrderStatusError)
	if f.h.ReloadWallet(buyer.Id).GiftAmount != 400 {
		t.Fatal("a repeated close refunded the gift credit again")
	}
}

func TestDeletePaymentMethodGuardsPendingOrders(t *testing.T) {
	f := newFacade(t)
	method := f.h.Payment("EPay", epayConfig)
	o := f.h.Order(&order.Order{OrderNo: "o-5", Status: order.StatusPending, PaymentId: method.Id, Method: "EPay"})

	assertCode(t, f.svc.DeletePaymentMethod(adminContext, &dto.DeletePaymentMethodRequest{Id: method.Id}), xerr.PaymentMethodHasPendingOrders)
	if err := f.svc.UpdateOrderStatus(adminContext, &dto.UpdateOrderStatusRequest{Id: o.Id, Status: order.StatusClosed}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeletePaymentMethod(adminContext, &dto.DeletePaymentMethodRequest{Id: method.Id}); err != nil {
		t.Fatalf("DeletePaymentMethod: %v", err)
	}
	var remaining int64
	if err := f.h.DB.Table("payment").Where("id = ?", method.Id).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("payment rows = %d (%v), want the method deleted", remaining, err)
	}
}

func TestCreatePaymentMethodValidatesFeeAndPlatform(t *testing.T) {
	f := newFacade(t)
	enabled := true
	_, err := f.svc.CreatePaymentMethod(adminContext, &dto.CreatePaymentMethodRequest{Name: "n", Platform: "Nope", Config: map[string]any{}, Enable: &enabled})
	assertCode(t, err, xerr.UnsupportedPaymentPlatform)
	_, err = f.svc.CreatePaymentMethod(adminContext, &dto.CreatePaymentMethodRequest{
		Name: "n", Platform: "EPay", FeeMode: 9, Enable: &enabled,
		Config: map[string]any{"pid": "1001", "url": "https://pay.example", "key": "secret", "type": "alipay"},
	})
	assertCode(t, err, xerr.InvalidPaymentFee)
}

// The facade routes a callback to the gateway of its platform; a platform
// without a gateway has no callback.
func TestPaymentCallbackStyles(t *testing.T) {
	f := newFacade(t)
	for _, platform := range []string{"EPay", "AlipayF2F", "Stripe", "Cryptomus", "Nano"} {
		if _, ok := f.svc.PaymentCallbackStyle(platform); !ok {
			t.Fatalf("%s has no callback style", platform)
		}
	}
	for _, platform := range []string{"balance", "Unknown", ""} {
		if _, ok := f.svc.PaymentCallbackStyle(platform); ok {
			t.Fatalf("%s must not accept callbacks", platform)
		}
	}
}

func TestGMPayMethodAvailableAfterCreation(t *testing.T) {
	f := newFacade(t)
	enabled := true
	created, err := f.svc.CreatePaymentMethod(adminContext, &dto.CreatePaymentMethodRequest{Name: "Crypto cashier", Platform: "GMPay", Config: map[string]string{"url": "https://gateway.example", "pid": "1000", "secret_key": "sample"}, Enable: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	methods, err := f.svc.GetAvailablePaymentMethods(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range methods.List {
		if method.Id == created.Id && method.Platform == "GMPay" {
			return
		}
	}
	t.Fatal("enabled GMPay method is missing from public checkout")
}

func TestPayPalWebhookSetup(t *testing.T) {
	f := newFacade(t)
	enabled, disabled := true, false
	cfg := map[string]string{"client_id": "client", "client_secret": "secret", "environment": "sandbox"}
	req := &dto.CreatePaymentMethodRequest{Name: "PayPal", Platform: "PayPal", Config: cfg, Domain: "https://panel.example", Enable: &enabled}
	_, err := f.svc.CreatePaymentMethod(adminContext, req)
	assertCode(t, err, xerr.InvalidPaymentConfig)
	req.Enable = &disabled
	draft, err := f.svc.CreatePaymentMethod(adminContext, req)
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.svc.GetPaymentMethodList(adminContext, &dto.GetPaymentMethodListRequest{Page: 1, Size: 10, Platform: "PayPal"})
	if err != nil || len(list.List) != 1 || !strings.HasPrefix(list.List[0].NotifyURL, "https://panel.example/v1/notify/PayPal/") {
		t.Fatalf("missing webhook setup URL: %v", err)
	}
	update := &dto.UpdatePaymentMethodRequest{Id: draft.Id, Name: "PayPal", Platform: "PayPal", Domain: req.Domain, Enable: &enabled, Config: cfg}
	_, err = f.svc.UpdatePaymentMethod(adminContext, update)
	assertCode(t, err, xerr.InvalidPaymentConfig)
	cfg["webhook_id"] = "WH-1"
	if _, err := f.svc.UpdatePaymentMethod(adminContext, update); err != nil {
		t.Fatal(err)
	}
	available, err := f.svc.GetAvailablePaymentMethods(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range available.List {
		if m.Id == draft.Id && m.Platform == "PayPal" {
			return
		}
	}
	t.Fatal("enabled PayPal checkout not available")
}
