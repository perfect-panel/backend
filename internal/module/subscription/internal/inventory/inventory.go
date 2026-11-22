// Package inventory reserves and restores plan stock for orders, each at most
// once per order through inbox markers. Only the module facade may reach it.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

// Reserve records the plan ID and units debited in the stock transaction.
// A duplicate marker rolls back a concurrent losing reservation.
func (s *Service) Reserve(ctx context.Context, orderNo string, subscribeID int64) error {
	return s.applyOnce(ctx, InventoryReserveConsumer, orderNo, func(tx repository.SubscriptionStore) (string, error) {
		reservation, err := tx.Subscribe().ReserveInventory(ctx, subscribeID)
		if err != nil {
			return "", err
		}
		plan := strconv.FormatInt(subscribeID, 10)
		switch reservation {
		case repository.InventoryDeducted:
			return plan + "|1", nil
		case repository.InventoryUnlimited:
			return plan + "|0", nil
		default:
			return "", ErrOutOfStock
		}
	})
}

// Restore returns the units recorded by Reserve. Even a zero-unit reservation
// gets a restore marker so retries after a plan edit cannot move its stock.
func (s *Service) Restore(ctx context.Context, orderNo string, subscribeID int64) error {
	mark, err := s.store.Inbox().Find(ctx, InventoryReserveConsumer, orderNo)
	if err != nil || mark == nil {
		return err
	}
	return s.applyOnce(ctx, InventoryRestoreConsumer, orderNo, func(tx repository.SubscriptionStore) (string, error) {
		_, units, recorded := strings.Cut(mark.Result, "|")
		if !recorded {
			// Historical markers only identified the plan. Preserve their
			// restoration semantics since the original mode is unknown.
			units = "1"
		}
		switch units {
		case "0":
			return "", nil
		case "1":
			return "", tx.Subscribe().RestoreInventory(ctx, subscribeID)
		default:
			return "", fmt.Errorf("invalid reserved inventory units %q for order %s", units, orderNo)
		}
	})
}

func (s *Service) applyOnce(ctx context.Context, consumer, orderNo string, apply func(repository.SubscriptionStore) (string, error)) error {
	mark, err := s.store.Inbox().Find(ctx, consumer, orderNo)
	if err != nil || mark != nil {
		return err
	}
	err = s.store.InSubscriptionTx(ctx, func(tx repository.SubscriptionStore) error {
		result, err := apply(tx)
		if err != nil {
			return err
		}
		return tx.Inbox().Insert(ctx, consumer, orderNo, result)
	})
	if err != nil {
		// A concurrent delivery may have committed the same operation while
		// this one waited for the plan lock (including its last stock unit).
		if mark, readErr := s.store.Inbox().Find(ctx, consumer, orderNo); readErr == nil && mark != nil {
			return nil
		}
	}
	return err
}
