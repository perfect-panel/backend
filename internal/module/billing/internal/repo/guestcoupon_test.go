package repo_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
)

// Guest orders carry no user until activation and keep their identity
// afterwards; the identity counts their coupon uses, pending and settled,
// while closed orders and other identities do not count.
func TestOrderRepoCountGuestCouponUsage(t *testing.T) {
	h := billingtest.New(t)
	ctx := context.Background()
	counter, ok := h.Store.Order().(portal.GuestCouponUsageCounter)
	if !ok {
		t.Fatal("the order repository does not count guest coupon uses")
	}
	for orderNo, o := range map[string]order.Order{
		"pending":   {Status: order.StatusPending, UserId: 0},
		"activated": {Status: order.StatusFinished, UserId: 42},
		"closed":    {Status: order.StatusClosed, UserId: 0},
	} {
		h.Order(&order.Order{OrderNo: orderNo, Status: o.Status, UserId: o.UserId, Coupon: "ONCE", GuestAuthType: "email", GuestIdentifier: "guest@example.com"})
	}
	h.Order(&order.Order{OrderNo: "other-identity", Status: order.StatusPending, Coupon: "ONCE", GuestAuthType: "email", GuestIdentifier: "other@example.com"})
	h.Order(&order.Order{OrderNo: "other-coupon", Status: order.StatusPending, Coupon: "TWICE", GuestAuthType: "email", GuestIdentifier: "guest@example.com"})

	count, err := counter.CountGuestCouponUsage(ctx, "email", "guest@example.com", "ONCE")
	if err != nil || count != 2 {
		t.Fatalf("CountGuestCouponUsage = (%d, %v), want the pending and the activated order", count, err)
	}
}
