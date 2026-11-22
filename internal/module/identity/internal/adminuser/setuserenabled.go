package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// SetUserEnabled enables or disables an account for a caller outside the
// admin panel (the Telegram bot's ban and unban). Like the admin edit it
// writes only the flag, so a concurrent change to the account survives, and
// it invalidates the same subscription-token caches and node user lists, so
// a ban takes effect at the service plane at once instead of after the
// caches' TTL. The caller flips the state it just read, so the caches are
// invalidated after every write rather than only on a detected change.
func (s *Service) SetUserEnabled(ctx context.Context, userID int64, enabled bool) error {
	err := s.deps.Store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		u, err := tx.User().FindOneForUpdate(ctx, userID)
		if err != nil {
			return err
		}
		if err := tx.User().UpdateColumns(ctx, userID, map[string]any{"enable": enabled}); err != nil {
			return err
		}
		if u.Enable == nil || *u.Enable != enabled {
			return s.rotateAccessEpoch(ctx, userID, enabled)
		}
		return nil
	})
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "set user %d enabled to %t", userID, enabled)
	}
	clearUserAccessCaches(ctx, s.deps, []int64{userID})
	return nil
}

// Rotate under the account row lock so a concurrent unban cannot commit
// before this revocation. A Redis outage must not prevent disabling an
// account: the database gate denies access and re-enabling requires a fresh
// epoch even for accounts disabled by older versions.
func (s *Service) rotateAccessEpoch(ctx context.Context, userID int64, enabled bool) error {
	if err := usersession.Revoke(ctx, s.deps.Redis, userID); err != nil {
		if enabled {
			return xerr.Wrapf(err, xerr.ERROR, "revoke sessions of user %d", userID)
		}
		logger.WithContext(ctx).Errorw("session rotation deferred until account reactivation", logger.Field("user_id", userID), logger.Field("error", err.Error()))
	}
	return nil
}
