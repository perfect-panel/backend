package subscription

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/subscription/internal/inventory"
)

// InventoryStore restricts order-driven inventory changes to subscription
// transactions and the durable inbox used to deduplicate them.
type InventoryStore = inventory.Store

// These persisted consumer identities must remain stable across deployments.
const (
	InventoryReserveConsumer = inventory.InventoryReserveConsumer
	InventoryRestoreConsumer = inventory.InventoryRestoreConsumer
)

// ErrOutOfStock reports that the plan has no inventory left to reserve.
var ErrOutOfStock = inventory.ErrOutOfStock

// Inventory owns reservation persistence. Callers pass only business inputs.
type Inventory interface {
	Reserve(ctx context.Context, orderNo string, subscribeID int64) error
	Restore(ctx context.Context, orderNo string, subscribeID int64) error
}

// NewInventory returns the plan inventory over its store.
func NewInventory(store InventoryStore) Inventory {
	return inventory.New(store)
}
