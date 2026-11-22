package checkout

import (
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Whether a subscription has a term is the subscription entity's rule
// (usersub.NoExpiry), not a raw epoch comparison: a no-limit marker written
// by a process in another zone reads back as a wall clock hours after the
// epoch, and such a subscription may still reset its traffic.
func TestResetTrafficFollowsTheEntityRuleForNoLimitSubscriptions(t *testing.T) {
	for name, marker := range map[string]time.Time{
		"epoch":                 time.UnixMilli(0),
		"epoch in another zone": time.UnixMilli(0).In(time.FixedZone("CST", 8*3600)),
		"shifted wall clock":    time.Date(1970, 1, 1, 8, 0, 0, 0, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			u, ctx := f.buyer(0)
			plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Replacement = 300 })
			sub := f.h.UserSubscription(u.Id, plan, func(s *usersub.Subscribe) { s.ExpireTime = marker })
			method := f.epay()

			resp, err := f.svc.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{UserSubscribeID: sub.Id, Payment: method.Id})
			if err != nil {
				t.Fatalf("ResetTraffic: %v", err)
			}
			if created := f.h.ReloadOrder(resp.OrderNo); created.Type != order.TypeResetTraffic || created.Amount != 300 {
				t.Fatalf("order = %+v, want a traffic reset of 300", created)
			}
		})
	}
	t.Run("elapsed term", func(t *testing.T) {
		f := newCheckoutFixture(t)
		u, ctx := f.buyer(0)
		plan := f.h.Plan(1000, func(p *subscribe.Subscribe) { p.Replacement = 300 })
		sub := f.h.UserSubscription(u.Id, plan, func(s *usersub.Subscribe) { s.ExpireTime = time.Now().Add(-time.Minute) })

		_, err := f.svc.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{UserSubscribeID: sub.Id, Payment: f.epay().Id})
		assertCode(t, err, xerr.SubscribeNotAvailable)
	})
}
