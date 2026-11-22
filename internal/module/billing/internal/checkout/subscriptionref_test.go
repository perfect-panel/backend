package checkout

import (
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// A renewal and a traffic reset name the subscription they apply to by its
// id, which a token rotation does not change, and still by its token for
// the fulfillment of older releases; a rotation between the order and its
// payment must not orphan the paid order.
func TestRenewalAndResetReferenceTheSubscriptionById(t *testing.T) {
	f := newCheckoutFixture(t)
	u, ctx := f.buyer(0)
	plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Replacement = 300 })
	sub := f.h.UserSubscription(u.Id, plan, func(s *usersub.Subscribe) { s.OrderId = 77 })
	method := f.epay()

	renewal, err := f.svc.Renewal(ctx, &dto.RenewalOrderRequest{UserSubscribeID: sub.Id, Quantity: 1, Payment: method.Id})
	if err != nil {
		t.Fatalf("Renewal: %v", err)
	}
	if o := f.h.ReloadOrder(renewal.OrderNo); o.UserSubscribeId != sub.Id || o.SubscribeToken != sub.Token || o.SubscribeId != plan.Id || o.ParentId != 77 {
		t.Fatalf("renewal order = %+v, want it bound to subscription %d by id and token", o, sub.Id)
	}
	reset, err := f.svc.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{UserSubscribeID: sub.Id, Payment: method.Id})
	if err != nil {
		t.Fatalf("ResetTraffic: %v", err)
	}
	if o := f.h.ReloadOrder(reset.OrderNo); o.UserSubscribeId != sub.Id || o.SubscribeToken != sub.Token || o.SubscribeId != plan.Id || o.ParentId != 77 {
		t.Fatalf("reset order = %+v, want it bound to subscription %d by id and token", o, sub.Id)
	}
}
