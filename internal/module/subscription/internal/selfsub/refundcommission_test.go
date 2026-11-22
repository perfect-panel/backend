package selfsub

import (
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

const (
	refundBuyer int64 = 7
	refundPlan  int64 = 9
	refundOrder int64 = 1
)

// paidSubscription is a balance-paid subscription (3000 from the balance,
// 1000 from the gift amount) whose refund is exactly 3000: it starts later,
// so the refundable share is the traffic left, three quarters.
func (f *fixture) paidSubscription(t *testing.T) *usersub.Subscribe {
	t.Helper()
	allow := true
	f.Plan(t, subscribe.Subscribe{Id: refundPlan, UnitTime: "Month", AllowDeduction: &allow})
	start := time.Now().Add(time.Hour)
	sub := f.Subscription(t, usersub.Subscribe{
		UserId: refundBuyer, OrderId: refundOrder, SubscribeId: refundPlan, StartTime: start, ExpireTime: start.AddDate(0, 1, 0),
		Traffic: 1000, Upload: 250, Status: usersub.SubscribeStatusActive,
	})
	f.orders[refundOrder] = &order.Details{Id: refundOrder, UserId: refundBuyer, OrderNo: "A", Method: "balance", Amount: 3000, GiftAmount: 1000, Commission: 800}
	return sub
}

// cancelledSubscription is a subscription whose cancellation committed with
// the given marker while its refund never did.
func (f *fixture) cancelledSubscription(t *testing.T, marker string) *usersub.Subscribe {
	t.Helper()
	f.Plan(t, subscribe.Subscribe{Id: refundPlan})
	sub := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, OrderId: refundOrder, SubscribeId: refundPlan, Status: usersub.SubscribeStatusDeducted})
	f.cancelled(t, sub.Id, marker)
	return sub
}

// The quote is what the cancellation refunds: the billing stage is asked once
// to settle the unused share of the buyer's order. A repeated request pays
// nothing twice.
func TestUnsubscribeRefundsTheUnusedShareOnce(t *testing.T) {
	f := newFixture(t)
	sub := f.paidSubscription(t)

	quote, err := f.svc.PreUnsubscribe(as(refundBuyer), &dto.PreUnsubscribeRequest{Id: sub.Id})
	if err != nil || quote.DeductionAmount != 3000 {
		t.Fatalf("PreUnsubscribe = %+v, %v; want 3000", quote, err)
	}
	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); err != nil {
		t.Fatal(err)
	}

	if got := f.Load(t, sub.Id).Status; got != usersub.SubscribeStatusDeducted {
		t.Fatalf("status = %d, want Deducted", got)
	}
	if result, ok := f.cancelMarker(t, sub.Id); !ok || result != "1|3000" {
		t.Fatalf("cancellation marker = %q, %v", result, ok)
	}
	want := refund{userID: refundBuyer, subID: sub.Id, orderID: refundOrder, amount: 3000}
	if got, ok := f.refunds.settled[sub.Id]; !ok || got != want || len(f.refunds.requests) != 1 {
		t.Fatalf("settled refund = %+v (%d requests), want %+v once", got, len(f.refunds.requests), want)
	}

	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); !errors.Is(err, errNotCancelable) {
		t.Fatalf("repeated Unsubscribe = %v, want errNotCancelable", err)
	}
	if len(f.refunds.requests) != 1 {
		t.Fatalf("the repeated request settled again: %+v", f.refunds.requests)
	}
}

// A refund that fails after the cancellation committed leaves the committed
// cancellation with the amount it recorded, and nothing settled; the retry
// resumes at the refund stage and settles that amount, once.
func TestUnsubscribeResumesAFailedRefund(t *testing.T) {
	f := newFixture(t)
	sub := f.paidSubscription(t)
	f.refunds.failNext = 1

	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); !errors.Is(err, errWalletUnavailable) {
		t.Fatalf("Unsubscribe = %v, want the wallet failure", err)
	}
	if got := f.Load(t, sub.Id).Status; got != usersub.SubscribeStatusDeducted {
		t.Fatalf("status = %d, want the committed cancellation", got)
	}
	if result, ok := f.cancelMarker(t, sub.Id); !ok || result != "1|3000" {
		t.Fatalf("cancellation marker = %q, %v", result, ok)
	}
	if _, ok := f.refunds.settled[sub.Id]; ok {
		t.Fatal("the failed refund was settled")
	}

	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); err != nil {
		t.Fatalf("retry = %v", err)
	}
	want := refund{userID: refundBuyer, subID: sub.Id, orderID: refundOrder, amount: 3000}
	if got := f.refunds.settled[sub.Id]; got != want || len(f.refunds.requests) != 2 {
		t.Fatalf("settled refund after the retry = %+v (%d requests), want %+v", got, len(f.refunds.requests), want)
	}
	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); !errors.Is(err, errNotCancelable) {
		t.Fatalf("second retry = %v, want errNotCancelable", err)
	}
	if len(f.refunds.requests) != 2 {
		t.Fatalf("the second retry settled again: %+v", f.refunds.requests)
	}
}

// A cancellation that committed while its refund never did resumes from its
// marker: the retry hands the billing stage the order and the amount the
// cancellation recorded, even one an older formula recorded above what was
// paid. Billing caps the refund at what was paid.
func TestUnsubscribeResumesTheRecordedRefund(t *testing.T) {
	f := newFixture(t)
	sub := f.cancelledSubscription(t, "1|72900")

	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); err != nil {
		t.Fatal(err)
	}
	want := refund{userID: refundBuyer, subID: sub.Id, orderID: refundOrder, amount: 72900}
	if got := f.refunds.settled[sub.Id]; got != want || len(f.refunds.requests) != 1 {
		t.Fatalf("settled refund = %+v (%d requests), want %+v", got, len(f.refunds.requests), want)
	}
}
