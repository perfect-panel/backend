package adminuser

import (
	"context"

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
	if err := s.deps.Users.UpdateColumns(ctx, userID, map[string]any{"enable": enabled}); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "set user %d enabled to %t", userID, enabled)
	}
	clearUserAccessCaches(ctx, s.deps, []int64{userID})
	return nil
}
