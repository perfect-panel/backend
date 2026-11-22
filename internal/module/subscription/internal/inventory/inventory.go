// Package inventory reserves and restores plan stock for orders, each at most
// once per order through inbox markers. Only the module facade may reach it.
package inventory

import (
	"context"
	"errors"
	"strconv"

	"github.com/perfect-panel/backend/internal/repository"
)

// Inbox consumers for the plan-inventory lifecycle (ADR-001 step 2).
// Reservation and restoration are subscription-domain writes that used to sit
// inside billing transactions; the idempotent inbox keys them by order number
// so the purchase flow, the activation safety net and the close-order
// compensation can each run at-least-once without double-moving stock.
const (
	InventoryReserveConsumer = "subscription.inventory_reserve"
	InventoryRestoreConsumer = "subscription.inventory_restore"
)

// ErrOutOfStock reports that the plan has no inventory left to reserve.
var ErrOutOfStock = errors.New("subscribe out of stock")

// Store is the narrow persistence surface the inventory lifecycle needs;
// the repository store satisfies it structurally.
type Store interface {
	Inbox() repository.InboxRepo
	InSubscriptionTx(ctx context.Context, fn func(repository.SubscriptionStore) error) error
}

type Service struct {
	store Store
}

func New(store Store) *Service { return &Service{store: store} }

// Reserve commits one unit and its inbox marker in the same subscription
// transaction. A duplicate marker rolls back a concurrent losing reservation.
func (s *Service) Reserve(ctx context.Context, orderNo string, subscribeID int64) error {
	store := s.store
	mark, err := store.Inbox().Find(ctx, InventoryReserveConsumer, orderNo)
	if err != nil {
		return err
	}
	if mark != nil {
		return nil
	}
	return store.InSubscriptionTx(ctx, func(tx repository.SubscriptionStore) error {
		reserved, err := tx.Subscribe().ReserveInventory(ctx, subscribeID)
		if err != nil {
			return err
		}
		if !reserved {
			return ErrOutOfStock
		}
		return tx.Inbox().Insert(ctx, InventoryReserveConsumer, orderNo, strconv.FormatInt(subscribeID, 10))
	})
}

// Restore returns the order's reserved unit exactly once. Orders
// that never reserved (stock-out compensation, historical orders) are a
// no-op, and a second restoration attempt is absorbed by the restore marker.
func (s *Service) Restore(ctx context.Context, orderNo string, subscribeID int64) error {
	store := s.store
	reserveMark, err := store.Inbox().Find(ctx, InventoryReserveConsumer, orderNo)
	if err != nil {
		return err
	}
	if reserveMark == nil {
		return nil
	}
	restoreMark, err := store.Inbox().Find(ctx, InventoryRestoreConsumer, orderNo)
	if err != nil {
		return err
	}
	if restoreMark != nil {
		return nil
	}
	return store.InSubscriptionTx(ctx, func(tx repository.SubscriptionStore) error {
		if err := tx.Subscribe().RestoreInventory(ctx, subscribeID); err != nil {
			return err
		}
		return tx.Inbox().Insert(ctx, InventoryRestoreConsumer, orderNo, "")
	})
}
