package checkout

import (
	"errors"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The renewal preview refuses what the renewal refuses: a refunded or
// stopped subscription, and one a payment provider manages.
func TestPreCreateOrderAppliesTheRenewalRules(t *testing.T) {
	plan := &subscribe.Subscribe{Id: 10, UnitPrice: 1000, Sell: boolPtr(true), Inventory: -1}
	renewable := func(status uint8, source string) *usersub.Subscribe {
		return &usersub.Subscribe{Id: 22, UserId: 42, SubscribeId: 10, Status: status, EntitlementSource: source}
	}
	preview := func(sub *usersub.Subscribe) error {
		svc := NewService(Deps{UserSubs: &policyUserSubs{subscription: sub}, Plans: policyPlans{subscribe: plan}})
		_, err := svc.PreCreateOrder(ownerContext(42), &dto.PurchaseOrderRequest{SubscribeId: 10, Quantity: 1, UserSubscribeId: 22})
		return err
	}

	for _, status := range []uint8{usersub.SubscribeStatusDeducted, usersub.SubscribeStatusStopped} {
		assertSubscribeNotAvailable(t, "renewal preview", status, preview(renewable(status, "")))
	}
	if err := preview(renewable(usersub.SubscribeStatusActive, "apple")); !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("preview of a provider-managed subscription: %v, want ErrProviderManaged", err)
	}
	if err := preview(renewable(usersub.SubscribeStatusExpired, "")); err != nil {
		t.Fatalf("preview of an expired local subscription: %v, want a price", err)
	}
	if err := preview(&usersub.Subscribe{Id: 22, UserId: 43, SubscribeId: 10}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("preview of another user's subscription: %v, want InvalidAccess", err)
	}
}
