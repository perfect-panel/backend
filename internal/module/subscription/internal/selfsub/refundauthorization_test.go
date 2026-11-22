package selfsub

import (
	"context"
	"errors"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// assertUntouched fails when a refused request changed the subscription or
// started either cancellation stage.
func (f *fixture) assertUntouched(t *testing.T, sub *usersub.Subscribe) {
	t.Helper()
	if got := f.Load(t, sub.Id); got.Status != sub.Status {
		t.Fatalf("status = %d, want %d", got.Status, sub.Status)
	}
	if _, ok := f.cancelMarker(t, sub.Id); ok {
		t.Fatalf("a refused request wrote the %s marker", unsubscribeCancelConsumer)
	}
	if len(f.refunds.requests) != 0 {
		t.Fatalf("a refused request settled refunds: %+v", f.refunds.requests)
	}
}

// Only the owner may quote or cancel a subscription.
func TestUnsubscribeIsForTheOwner(t *testing.T) {
	f := newFixture(t)
	sub := f.paidSubscription(t)
	for name, ctx := range map[string]context.Context{"another user": as(refundBuyer + 1), "anonymous": context.Background()} {
		if _, err := f.svc.PreUnsubscribe(ctx, &dto.PreUnsubscribeRequest{Id: sub.Id}); xerr.CodeOf(err) != xerr.InvalidAccess {
			t.Fatalf("%s: PreUnsubscribe = %v, want InvalidAccess", name, err)
		}
		if err := f.svc.Unsubscribe(ctx, &dto.UnsubscribeRequest{Id: sub.Id}); xerr.CodeOf(err) != xerr.InvalidAccess {
			t.Fatalf("%s: Unsubscribe = %v, want InvalidAccess", name, err)
		}
	}
	f.assertUntouched(t, sub)
}

// A provider-managed subscription is cancelled through its provider, and one
// that already ended has nothing left to cancel.
func TestUnsubscribeRefusesProviderManagedAndEndedSubscriptions(t *testing.T) {
	f := newFixture(t)
	f.Plan(t, subscribe.Subscribe{Id: refundPlan})
	future := time.Now().Add(24 * time.Hour)
	provider := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, ExpireTime: future, Status: usersub.SubscribeStatusActive, EntitlementSource: "apple"})
	ended := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, Status: usersub.SubscribeStatusExpired})
	stopped := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, ExpireTime: future, Status: usersub.SubscribeStatusStopped})
	ctx := as(refundBuyer)

	if _, err := f.svc.PreUnsubscribe(ctx, &dto.PreUnsubscribeRequest{Id: provider.Id}); !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("provider quote = %v", err)
	}
	if err := f.svc.Unsubscribe(ctx, &dto.UnsubscribeRequest{Id: provider.Id}); !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("provider cancellation = %v", err)
	}
	for _, sub := range []*usersub.Subscribe{ended, stopped} {
		if err := f.svc.Unsubscribe(ctx, &dto.UnsubscribeRequest{Id: sub.Id}); !errors.Is(err, errNotCancelable) {
			t.Fatalf("cancelling status %d = %v, want errNotCancelable", sub.Status, err)
		}
	}
	// A Deducted subscription without a cancellation marker was not deducted
	// by a cancellation: there is no refund to resume.
	deducted := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, Status: usersub.SubscribeStatusDeducted})
	if err := f.svc.Unsubscribe(ctx, &dto.UnsubscribeRequest{Id: deducted.Id}); !errors.Is(err, errNotCancelable) {
		t.Fatalf("cancelling a deducted subscription = %v, want errNotCancelable", err)
	}
	for _, sub := range []*usersub.Subscribe{provider, ended, stopped, deducted} {
		f.assertUntouched(t, sub)
	}
	if err := f.svc.Unsubscribe(ctx, &dto.UnsubscribeRequest{Id: 404}); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("cancelling a missing subscription = %v", err)
	}
}

// A plan deleted after the purchase takes its refund rules with it: quoting
// or cancelling reports that instead of dereferencing the missing plan.
func TestUnsubscribeRefusesSubscriptionsWhosePlanIsGone(t *testing.T) {
	f := newFixture(t)
	sub := f.paidSubscription(t)
	if err := f.DB.Delete(&subscribe.Subscribe{}, refundPlan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PreUnsubscribe(as(refundBuyer), &dto.PreUnsubscribeRequest{Id: sub.Id}); xerr.CodeOf(err) != xerr.SubscribeNotAvailable {
		t.Fatalf("PreUnsubscribe = %v, want SubscribeNotAvailable", err)
	}
	if err := f.svc.Unsubscribe(as(refundBuyer), &dto.UnsubscribeRequest{Id: sub.Id}); xerr.CodeOf(err) != xerr.SubscribeNotAvailable {
		t.Fatalf("Unsubscribe = %v, want SubscribeNotAvailable", err)
	}
	f.assertUntouched(t, sub)
}

// A plan row whose deduction flag was never set allows no deduction.
func TestUnsubscribeRefusesPlansWithoutADeductionFlag(t *testing.T) {
	f := newFixture(t)
	sub := f.paidSubscription(t)
	if err := f.DB.Model(&subscribe.Subscribe{}).Where("id = ?", refundPlan).Update("allow_deduction", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PreUnsubscribe(as(refundBuyer), &dto.PreUnsubscribeRequest{Id: sub.Id}); xerr.CodeOf(err) != xerr.SubscribeNotAvailable {
		t.Fatalf("PreUnsubscribe = %v, want SubscribeNotAvailable", err)
	}
	f.assertUntouched(t, sub)
}
