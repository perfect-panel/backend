package checkout

import (
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Refunded and administrator-stopped subscriptions come back only through an
// administrator, not through a renewal or a (possibly free) traffic reset.
func TestRenewalAndResetRejectRefundedOrStoppedSubscription(t *testing.T) {
	for _, status := range []uint8{usersub.SubscribeStatusDeducted, usersub.SubscribeStatusStopped} {
		svc := NewService(Deps{UserSubs: ownershipUserSubs{subscribe: &usersub.SubscribeDetails{
			Id: 22, UserId: 11, Status: status, ExpireTime: time.Now().Add(24 * time.Hour),
			Subscribe: &subscribe.Subscribe{Id: 5, Replacement: 0},
		}}})

		_, err := svc.Renewal(ownerContext(11), &dto.RenewalOrderRequest{UserSubscribeID: 22, Quantity: 1})
		assertSubscribeNotAvailable(t, "renewal", status, err)
		_, err = svc.ResetTraffic(ownerContext(11), &dto.ResetTrafficOrderRequest{UserSubscribeID: 22})
		assertSubscribeNotAvailable(t, "reset traffic", status, err)
	}
}

func assertSubscribeNotAvailable(t *testing.T, action string, status uint8, err error) {
	t.Helper()
	if xerr.CodeOf(err) != xerr.SubscribeNotAvailable {
		t.Fatalf("%s of a subscription in status %d: error = %v, want SubscribeNotAvailable", action, status, err)
	}
}
