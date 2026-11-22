// Package guestaccount owns the account-creation stage of paid guest orders.
package guestaccount

import (
	"context"
	"fmt"
	"strconv"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Consumer is persisted; changing it would recreate accounts during replay.
const Consumer = "identity.guest_account"

// Store is the persistence guest accounts need: the markers of the orders
// whose account exists, and the identity transaction that creates an account
// together with its marker.
type Store interface {
	Inbox() repository.InboxRepo
	repository.IdentityTransactor
}

// Command is the account a paid guest order asks for.
type Command struct {
	OrderNo      string
	AuthType     string
	Identifier   string
	PasswordHash string
	// LegacyPassword supports orders written before password hashes were
	// persisted. It is never written back or included in logs.
	LegacyPassword string
	InviteCode     string
}

// Service creates the accounts of paid guest orders.
type Service struct{ store Store }

// New builds the service over the store it persists to.
func New(store Store) *Service { return &Service{store: store} }

// FindGuestAccount lets billing recover the committed account even when its
// historical Redis checkout snapshot has already expired.
func (s *Service) FindGuestAccount(ctx context.Context, orderNo string) (int64, bool, error) {
	mark, err := s.store.Inbox().Find(ctx, Consumer, orderNo)
	if err != nil || mark == nil {
		return 0, false, err
	}
	id, err := strconv.ParseInt(mark.Result, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("corrupt guest account marker %q: %w", mark.Result, err)
	}
	return id, true, nil
}

// EnsureGuestAccount returns the account of the order, creating it on the
// first call: the account, its unverified identity and the order's marker
// commit in one identity transaction, so a replay finds the account instead
// of creating another.
func (s *Service) EnsureGuestAccount(ctx context.Context, command Command) (int64, error) {
	if id, found, err := s.FindGuestAccount(ctx, command.OrderNo); err != nil || found {
		return id, err
	}
	// A guest names an identifier nobody has verified. Only email and mobile
	// are accepted: their owner can take the account over through a code,
	// whereas a provider id would pre-claim someone else's OAuth sign-in.
	if command.AuthType != identifier.Email && command.AuthType != identifier.Mobile {
		return 0, fmt.Errorf("guest order %s names unsupported auth type %q", command.OrderNo, command.AuthType)
	}
	passwordHash := command.PasswordHash
	if passwordHash == "" {
		if command.LegacyPassword == "" {
			return 0, fmt.Errorf("guest order password hash is missing")
		}
		passwordHash = password.EncodePassWord(command.LegacyPassword)
	}
	u := &user.User{Password: passwordHash, Algo: password.PasswordAlgoForHash(passwordHash)}
	err := s.store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		// The identifier stays unverified: whoever proves it later takes
		// the account over through a code. A guest account is not a
		// registration, so it emits no registration event.
		if err := account.Create(ctx, tx, account.New{
			User:       u,
			Identities: []user.AuthMethods{{AuthType: command.AuthType, AuthIdentifier: command.Identifier}},
		}); err != nil {
			return err
		}
		if command.InviteCode != "" {
			if referer, err := tx.User().FindOneByReferCode(ctx, command.InviteCode); err == nil {
				u.RefererId = referer.Id
				if err := tx.User().UpdateColumns(ctx, u.Id, map[string]any{"referer_id": u.RefererId}); err != nil {
					return err
				}
			} else {
				logger.WithContext(ctx).Error("Find referer failed", logger.Field("error", err.Error()), logger.Field("refer_code", command.InviteCode))
			}
		}
		return tx.Inbox().Insert(ctx, Consumer, command.OrderNo, strconv.FormatInt(u.Id, 10))
	})
	if err != nil {
		return 0, err
	}
	return u.Id, nil
}
