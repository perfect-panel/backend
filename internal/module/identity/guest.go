package identity

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/internal/guestaccount"
)

// GuestAccountCommand is the account a paid guest order asks for.
type GuestAccountCommand = guestaccount.Command

// GuestAccounts is the identity-owned capability used by paid guest orders.
// Persistence is supplied once at construction, never by an operation caller.
type GuestAccounts interface {
	FindGuestAccount(context.Context, string) (int64, bool, error)
	EnsureGuestAccount(context.Context, GuestAccountCommand) (int64, error)
}

// NewGuestAccounts builds the guest accounts over the store they persist to.
func NewGuestAccounts(store guestaccount.Store) GuestAccounts {
	return guestaccount.New(store)
}
