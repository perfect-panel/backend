package adminorder

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// createFixture runs the administrator's order creation over the real
// repositories.
type createFixture struct {
	t      *testing.T
	h      *billingtest.Harness
	svc    *Service
	buyer  *user.User
	plan   *subscribe.Subscribe
	method *payment.Payment
}

func newCreateFixture(t *testing.T, adjust ...func(*Deps)) *createFixture {
	t.Helper()
	h := billingtest.New(t)
	deps := Deps{
		Orders: h.Store.Order(), Payments: h.Store.Payment(), Coupons: h.Store.Coupon(), UserSubs: h.Store.UserSubscription(),
		Inventory: subscription.NewInventory(h.Store), Tx: h.Store, Queue: &billingtest.Queue{}, Plans: h.Store.Subscribe(),
	}
	for _, fn := range adjust {
		fn(&deps)
	}
	return &createFixture{
		t: t, h: h, svc: NewService(deps), buyer: h.User(),
		plan:   h.Plan(1000, func(p *subscribe.Subscribe) { p.Inventory = 3 }),
		method: h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`),
	}
}

// subscribeOrder is a valid new-subscription order request.
func (f *createFixture) subscribeOrder() *dto.CreateOrderRequest {
	return &dto.CreateOrderRequest{UserId: f.buyer.Id, Type: order.TypeSubscribe, Quantity: 1, Price: 1000, Amount: 1000, FeeAmount: 0, PaymentId: f.method.Id, SubscribeId: f.plan.Id}
}

func (f *createFixture) orders() []*order.Order { return f.h.Orders(f.buyer.Id) }

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// An administrator's order follows the rules a buyer's follows: a type the
// activation knows, at least one unit, a plan or subscription to apply to,
// and no trade number of the administrator's choosing.
func TestCreateValidatesTheOrder(t *testing.T) {
	for name, tt := range map[string]struct {
		mutate func(*dto.CreateOrderRequest)
		code   uint32
	}{
		"unknown type":                    {func(r *dto.CreateOrderRequest) { r.Type = 5 }, xerr.InvalidParams},
		"negative quantity":               {func(r *dto.CreateOrderRequest) { r.Quantity = -1 }, xerr.InvalidParams},
		"subscription without plan":       {func(r *dto.CreateOrderRequest) { r.SubscribeId = 0 }, xerr.InvalidParams},
		"renewal without subscription":    {func(r *dto.CreateOrderRequest) { r.Type = order.TypeRenewal }, xerr.InvalidParams},
		"reset without subscription":      {func(r *dto.CreateOrderRequest) { r.Type = order.TypeResetTraffic }, xerr.InvalidParams},
		"client-supplied trade number":    {func(r *dto.CreateOrderRequest) { r.TradeNo = "pi_attacker" }, xerr.InvalidParams},
		"coupon on a recharge":            {func(r *dto.CreateOrderRequest) { r.Type, r.SubscribeId, r.Coupon = order.TypeRecharge, 0, "SAVE" }, xerr.InvalidParams},
		"non-pending initial status":      {func(r *dto.CreateOrderRequest) { r.Status = order.StatusPaid }, xerr.InvalidInitialOrderStatus},
		"unknown payment method":          {func(r *dto.CreateOrderRequest) { r.PaymentId = 999 }, xerr.PaymentMethodNotFound},
		"unknown coupon":                  {func(r *dto.CreateOrderRequest) { r.Coupon = "NOPE" }, xerr.CouponNotExist},
		"coupon of another plan":          {func(r *dto.CreateOrderRequest) { r.Coupon = "OTHER" }, xerr.CouponNotApplicable},
		"renewal of missing subscription": {func(r *dto.CreateOrderRequest) { r.Type, r.UserSubscribeId = order.TypeRenewal, 404 }, xerr.InvalidParams},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCreateFixture(t)
			f.h.Coupon("OTHER", func(c *coupon.Coupon) { c.Subscribe = "999" })
			req := f.subscribeOrder()
			tt.mutate(req)
			assertCode(t, f.svc.Create(context.Background(), req), tt.code)
			if len(f.orders()) != 0 || f.h.ReloadPlan(f.plan.Id).Inventory != 3 {
				t.Fatal("a refused order was stored or took stock")
			}
		})
	}
}

// A new subscription order holds its plan unit and its coupon use while it
// is pending, as a buyer's order does; a missing quantity is one unit.
func TestCreateReservesTheCouponAndThePlanUnit(t *testing.T) {
	f := newCreateFixture(t)
	f.h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5 })
	req := f.subscribeOrder()
	req.Quantity, req.Coupon = 0, "SAVE"

	if err := f.svc.Create(context.Background(), req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	orders := f.orders()
	if len(orders) != 1 {
		t.Fatalf("orders = %+v, want one", orders)
	}
	created := orders[0]
	if created.Status != order.StatusPending || created.Quantity != 1 || created.Coupon != "SAVE" || !created.CouponReserved || created.TradeNo != "" || created.Method != "EPay" {
		t.Fatalf("order = %+v, want a pending unit holding its coupon use", created)
	}
	if f.h.ReloadCoupon("SAVE").UsedCount != 1 || f.h.ReloadPlan(f.plan.Id).Inventory != 2 {
		t.Fatal("the order did not reserve its coupon use and plan unit")
	}
}

// The coupon's per-user limit binds the buyer the order is created for.
func TestCreateAppliesTheCouponUserLimit(t *testing.T) {
	f := newCreateFixture(t)
	f.h.Coupon("ONCE", func(c *coupon.Coupon) { c.Count = 5; c.UserLimit = 1 })
	f.h.Order(&order.Order{OrderNo: "earlier", UserId: f.buyer.Id, Status: order.StatusFinished, Coupon: "ONCE", SubscribeId: f.plan.Id})
	req := f.subscribeOrder()
	req.Coupon = "ONCE"

	assertCode(t, f.svc.Create(context.Background(), req), xerr.CouponInsufficientUsage)
	if f.h.ReloadCoupon("ONCE").UsedCount != 0 || len(f.orders()) != 1 {
		t.Fatal("the refused order reserved the coupon or was stored")
	}
}

// soldOut is the inventory of a plan another buyer just emptied.
type soldOut struct{}

func (soldOut) Reserve(context.Context, string, int64) error { return subscription.ErrOutOfStock }

// An order whose plan unit cannot be reserved is closed again and returns
// its coupon use.
func TestCreateClosesTheOrderWhenThePlanIsSoldOut(t *testing.T) {
	f := newCreateFixture(t, func(d *Deps) { d.Inventory = soldOut{} })
	f.h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5 })
	req := f.subscribeOrder()
	req.Coupon = "SAVE"

	assertCode(t, f.svc.Create(context.Background(), req), xerr.SubscribeOutOfStock)
	orders := f.orders()
	if len(orders) != 1 || orders[0].Status != order.StatusClosed || f.h.ReloadCoupon("SAVE").UsedCount != 0 {
		t.Fatalf("orders = %+v coupon uses = %d, want the order closed and its coupon use returned", orders, f.h.ReloadCoupon("SAVE").UsedCount)
	}
}

// A renewal or traffic reset applies to the subscription it names: the
// order carries the subscription's id, which a token rotation does not
// change, its token for older fulfillment, its plan and its original order.
// Another user's subscription and another plan are refused.
func TestCreateBindsRenewalsAndResetsToTheSubscription(t *testing.T) {
	f := newCreateFixture(t)
	sub := f.h.UserSubscription(f.buyer.Id, f.plan, func(s *usersub.Subscribe) { s.OrderId = 77 })
	for _, orderType := range []uint8{order.TypeRenewal, order.TypeResetTraffic} {
		req := &dto.CreateOrderRequest{UserId: f.buyer.Id, Type: orderType, Price: 1000, Amount: 1000, PaymentId: f.method.Id, UserSubscribeId: sub.Id}
		if err := f.svc.Create(context.Background(), req); err != nil {
			t.Fatalf("Create type %d: %v", orderType, err)
		}
	}
	orders := f.orders()
	if len(orders) != 2 {
		t.Fatalf("orders = %+v, want the renewal and the reset", orders)
	}
	for _, o := range orders {
		if o.UserSubscribeId != sub.Id || o.SubscribeToken != sub.Token || o.SubscribeId != f.plan.Id || o.ParentId != 77 || o.Quantity != 1 {
			t.Fatalf("order = %+v, want it bound to subscription %d", o, sub.Id)
		}
	}
	if f.h.ReloadPlan(f.plan.Id).Inventory != 3 {
		t.Fatal("a renewal or reset took a plan unit")
	}

	stranger := f.h.User()
	assertCode(t, f.svc.Create(context.Background(), &dto.CreateOrderRequest{
		UserId: stranger.Id, Type: order.TypeRenewal, Price: 1000, Amount: 1000, PaymentId: f.method.Id, UserSubscribeId: sub.Id,
	}), xerr.InvalidParams)
	assertCode(t, f.svc.Create(context.Background(), &dto.CreateOrderRequest{
		UserId: f.buyer.Id, Type: order.TypeRenewal, Price: 1000, Amount: 1000, PaymentId: f.method.Id, UserSubscribeId: sub.Id, SubscribeId: f.plan.Id + 1,
	}), xerr.InvalidParams)
	if len(f.orders()) != 2 || len(f.h.Orders(stranger.Id)) != 0 {
		t.Fatal("a refused renewal was stored")
	}
}
