package activation

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// refundFixture drives the activation workflow over the real subscription
// fulfillment and the real repositories.
type refundFixture struct {
	h        *billingtest.Harness
	workflow *Workflow
	buyer    *user.User
	plan     *subscribe.Subscribe
	method   *payment.Payment
}

func newRefundFixture(t *testing.T) *refundFixture {
	t.Helper()
	h := billingtest.New(t)
	subscriptions := subscription.New(subscription.Deps{
		Plans: h.Store.Subscribe(), UserSubs: h.Store.UserSubscription(), Orders: h.Store.Order(),
		Store: h.Store, Operations: h.Store, Inbox: h.Store.Inbox(),
		SingleModel: func() bool { return false },
	})
	stages := NewService(Deps{
		Orders: h.Store.Order(), Store: h.Store, Profiles: h.Store.User(),
		InvitePolicy: func() (uint8, bool) { return 0, false },
	})
	workflow := NewWorkflow(WorkflowDeps{Orders: h.Store.Order(), Profiles: h.Store.User(), Subscriptions: subscriptions}, stages)
	buyer := h.User()
	h.Wallet(buyer.Id, 500, 50)
	return &refundFixture{
		h: h, workflow: workflow, buyer: buyer, plan: h.Plan(1000),
		method: h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`),
	}
}

// paidRenewal seeds a gateway-paid renewal of sub that holds a coupon use and
// 200 of gift credit, as checkout and the payment callback leave it.
func (f *refundFixture) paidRenewal(orderNo string, sub *usersub.Subscribe) *order.Order {
	f.h.Coupon(orderNo, func(c *coupon.Coupon) { c.Count = 5; c.UsedCount = 1 })
	return f.h.Order(&order.Order{
		OrderNo: orderNo, UserId: f.buyer.Id, Type: order.TypeRenewal, Status: order.StatusPaid,
		Price: 1000, Amount: 800, GiftAmount: 200, Coupon: orderNo, CouponReserved: true,
		PaymentId: f.method.Id, Method: f.method.Platform, TradeNo: "trade-" + orderNo,
		SubscribeId: f.plan.Id, SubscribeToken: sub.Token,
	})
}

// assertRefunded checks the one refund of the order: the payment back on the
// balance, the gift credit back on the gift balance, each with its ledger
// entry, the coupon use released, the order closed with its event and no
// paid order left for the reconciler to alert on.
func (f *refundFixture) assertRefunded(t *testing.T, o *order.Order) {
	t.Helper()
	if w := f.h.ReloadWallet(f.buyer.Id); w.Balance != 1300 || w.GiftAmount != 250 {
		t.Fatalf("wallet = %+v, want the payment and the gift credit returned once", w)
	}
	balances := f.h.BalanceLogs(f.buyer.Id)
	if len(balances) != 1 || balances[0].Type != logEntity.BalanceTypeRefund || balances[0].Amount != 800 || balances[0].Balance != 1300 || balances[0].OrderNo != o.OrderNo {
		t.Fatalf("balance ledger = %+v, want one refund of 800", balances)
	}
	gifts := f.h.GiftLogs(f.buyer.Id)
	if len(gifts) != 1 || gifts[0].Type != logEntity.GiftTypeIncrease || gifts[0].Amount != 200 || gifts[0].Balance != 250 || gifts[0].OrderNo != o.OrderNo || gifts[0].Remark != unfulfillableRefundRemark {
		t.Fatalf("gift ledger = %+v, want one refund of 200", gifts)
	}
	if used := f.h.ReloadCoupon(o.OrderNo).UsedCount; used != 0 {
		t.Fatalf("coupon uses = %d, want the reservation released", used)
	}
	closed := f.h.ReloadOrder(o.OrderNo)
	events := f.h.Events(o.OrderNo)
	if closed.Status != order.StatusClosed || len(events) != 1 || events[0].EventType != "order.closed" {
		t.Fatalf("order = %+v events = %+v, want it closed with its event", closed, events)
	}
	paid, err := f.h.Store.Order().QueryOrdersByStatusAfterID(context.Background(), order.StatusPaid, 0, 10)
	if err != nil || len(paid) != 0 {
		t.Fatalf("paid orders = %v, %v; want none left for the reconciler", paid, err)
	}
}

// A renewal paid for a subscription that was stopped between checkout and
// payment can never be fulfilled. The activation refunds it, closes the order
// and succeeds; a redelivered activation refunds nothing more.
func TestActivateRefundsARenewalOfAStoppedSubscription(t *testing.T) {
	f := newRefundFixture(t)
	sub := f.h.UserSubscription(f.buyer.Id, f.plan, func(s *usersub.Subscribe) { s.Status = usersub.SubscribeStatusStopped })
	o := f.paidRenewal("renew-1", sub)

	for range 2 {
		if err := f.workflow.Activate(context.Background(), o.OrderNo); err != nil {
			t.Fatalf("Activate: %v", err)
		}
	}
	f.assertRefunded(t, o)
	var kept usersub.Subscribe
	if err := f.h.DB.First(&kept, sub.Id).Error; err != nil || kept.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("subscription = %+v, %v; want it left stopped", kept, err)
	}
}

// A local renewal of a subscription a payment provider manages now cannot be
// applied either; billing collected the money, so billing returns it.
func TestActivateRefundsARenewalOfAProviderManagedSubscription(t *testing.T) {
	f := newRefundFixture(t)
	sub := f.h.UserSubscription(f.buyer.Id, f.plan, func(s *usersub.Subscribe) { s.EntitlementSource = "apple" })
	o := f.paidRenewal("renew-2", sub)

	if err := f.workflow.Activate(context.Background(), o.OrderNo); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	f.assertRefunded(t, o)
}

// An order a payment provider collected is the provider's to settle: the
// provider-managed refusal does not refund it from billing's wallet, and the
// activation keeps failing for the reconciler.
func TestActivateDoesNotRefundAProviderCollectedOrder(t *testing.T) {
	f := newRefundFixture(t)
	o := f.h.Order(&order.Order{
		OrderNo: "apple-1", UserId: f.buyer.Id, Type: order.TypeSubscribe, Status: order.StatusPaid,
		Amount: 800, Method: "AppleIAP", TradeNo: "txn-1", SubscribeId: f.plan.Id,
	})

	err := f.workflow.Activate(context.Background(), o.OrderNo)
	if !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("Activate = %v, want ErrProviderManaged", err)
	}
	if w := f.h.ReloadWallet(f.buyer.Id); w.Balance != 500 || f.h.ReloadOrder(o.OrderNo).Status != order.StatusPaid {
		t.Fatalf("wallet = %+v status = %d, want the provider's order untouched", w, f.h.ReloadOrder(o.OrderNo).Status)
	}
}
