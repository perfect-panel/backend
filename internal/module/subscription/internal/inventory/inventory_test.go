package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
)

const inventoryPlan int64 = 9

// newInventoryFixture stores plan 9 with the given stock (-1 is unlimited).
func newInventoryFixture(t *testing.T, stock int64) (*subtest.Fixture, *Service) {
	t.Helper()
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: inventoryPlan})
	// A zero stock would be replaced by the column default on insert.
	if err := f.DB.Model(&subscribe.Subscribe{}).Where("id = ?", inventoryPlan).Update("inventory", stock).Error; err != nil {
		t.Fatal(err)
	}
	return f, New(f.Store)
}

func stock(t *testing.T, f *subtest.Fixture) int64 {
	t.Helper()
	var plan subscribe.Subscribe
	if err := f.DB.First(&plan, inventoryPlan).Error; err != nil {
		t.Fatal(err)
	}
	return plan.Inventory
}

func marked(t *testing.T, f *subtest.Fixture, consumer, orderNo string) bool {
	t.Helper()
	record, err := f.Store.Inbox().Find(context.Background(), consumer, orderNo)
	if err != nil {
		t.Fatal(err)
	}
	return record != nil
}

func TestReserveInventoryOnceIsIdempotent(t *testing.T) {
	f, svc := newInventoryFixture(t, 1)
	for range 2 {
		if err := svc.Reserve(context.Background(), "order-1", inventoryPlan); err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}
	if got := stock(t, f); got != 0 || !marked(t, f, InventoryReserveConsumer, "order-1") {
		t.Fatalf("stock = %d after a replayed reservation, want one unit taken and marked", got)
	}
}

func TestReserveInventoryOnceReportsOutOfStock(t *testing.T) {
	f, svc := newInventoryFixture(t, 0)
	if err := svc.Reserve(context.Background(), "order-2", inventoryPlan); !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("expected ErrOutOfStock, got %v", err)
	}
	if marked(t, f, InventoryReserveConsumer, "order-2") || stock(t, f) != 0 {
		t.Fatal("out-of-stock attempt must not leave a reserve marker or change the stock")
	}
}

// An unlimited plan has nothing to count: the reservation is recorded, the
// stock stays unlimited.
func TestReserveInventoryUnlimitedPlan(t *testing.T) {
	f, svc := newInventoryFixture(t, -1)
	if err := svc.Reserve(context.Background(), "order-4", inventoryPlan); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := svc.Restore(context.Background(), "order-4", inventoryPlan); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := stock(t, f); got != -1 || !marked(t, f, InventoryRestoreConsumer, "order-4") {
		t.Fatalf("unlimited stock = %d", got)
	}
}

func TestRestoreInventoryOnceSkipsUnreservedOrders(t *testing.T) {
	f, svc := newInventoryFixture(t, 1)
	if err := svc.Restore(context.Background(), "never-reserved", inventoryPlan); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := stock(t, f); got != 1 || marked(t, f, InventoryRestoreConsumer, "never-reserved") {
		t.Fatalf("an order that never reserved restored stock: %d", got)
	}
}

func TestRestoreInventoryOnceRestoresExactlyOnce(t *testing.T) {
	f, svc := newInventoryFixture(t, 1)
	if err := svc.Reserve(context.Background(), "order-3", inventoryPlan); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	for range 2 {
		if err := svc.Restore(context.Background(), "order-3", inventoryPlan); err != nil {
			t.Fatalf("restore: %v", err)
		}
	}
	if got := stock(t, f); got != 1 || !marked(t, f, InventoryRestoreConsumer, "order-3") {
		t.Fatalf("stock = %d, want 1 (reserve then restore once)", got)
	}
}

// A reservation that fails to commit leaves neither the unit taken nor its
// marker, so the retry reserves.
func TestReserveInventoryRollsBackWithItsMarker(t *testing.T) {
	f, svc := newInventoryFixture(t, 2)
	f.Store.FailNextCommits(1)
	if err := svc.Reserve(context.Background(), "order-5", inventoryPlan); !errors.Is(err, subtest.ErrInjectedRollback) {
		t.Fatalf("reserve = %v, want the rollback", err)
	}
	if got := stock(t, f); got != 2 || marked(t, f, InventoryReserveConsumer, "order-5") {
		t.Fatalf("the rolled-back reservation left stock %d", got)
	}
	if err := svc.Reserve(context.Background(), "order-5", inventoryPlan); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := stock(t, f); got != 1 {
		t.Fatalf("stock after the retry = %d, want 1", got)
	}
}
