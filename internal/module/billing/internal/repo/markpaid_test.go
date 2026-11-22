package repo_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
)

// A settlement carries the trade it verified. An order that claimed another
// gateway payment meanwhile (a Stripe intent, a Cryptomus invoice) keeps it;
// only its own trade, or none, may be settled.
func TestOrderRepoMarkOrderPaidKeepsAnotherClaimedTrade(t *testing.T) {
	h := billingtest.New(t)
	ctx := context.Background()
	repo := h.Store.Order()
	h.Order(&order.Order{OrderNo: "claimed", Status: order.StatusPending, TradeNo: "pi_1"})
	h.Order(&order.Order{OrderNo: "unclaimed", Status: order.StatusPending})

	if updated, err := repo.MarkOrderPaid(ctx, "claimed", "pi_2"); err != nil || updated {
		t.Fatalf("MarkOrderPaid with another trade = (%t, %v), want no transition", updated, err)
	}
	if o := h.ReloadOrder("claimed"); o.Status != order.StatusPending || o.TradeNo != "pi_1" {
		t.Fatalf("order = %+v, want it untouched", o)
	}
	if updated, err := repo.MarkOrderPaid(ctx, "claimed", "pi_1"); err != nil || !updated {
		t.Fatalf("MarkOrderPaid with the claimed trade = (%t, %v), want the transition", updated, err)
	}
	if updated, err := repo.MarkOrderPaid(ctx, "unclaimed", "T-1"); err != nil || !updated {
		t.Fatalf("MarkOrderPaid of an unclaimed order = (%t, %v), want the transition", updated, err)
	}
	if o := h.ReloadOrder("unclaimed"); o.Status != order.StatusPaid || o.TradeNo != "T-1" {
		t.Fatalf("order = %+v, want it paid with T-1", o)
	}
}
