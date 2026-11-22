package adminuser

import (
	"context"
	"slices"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// administrators is the read of the administrator rows the guard needs; the
// full repository and the identity transaction's store both provide it.
type administrators interface {
	QueryAdminUsers(ctx context.Context) ([]*user.User, error)
}

// ensureAnotherAdministrator refuses an edit that would leave the panel
// without an enabled administrator: demoting, disabling or deleting the
// accounts in losing is allowed only while at least one other enabled
// administrator remains. Nobody could then re-enable or promote anyone
// through the API, so the lock-out would be permanent.
func ensureAnotherAdministrator(ctx context.Context, admins administrators, losing ...int64) error {
	all, err := admins.QueryAdminUsers(ctx)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "query the administrators")
	}
	affected := false
	for _, admin := range all {
		if !isEnabledAdministrator(admin) {
			continue
		}
		if slices.Contains(losing, admin.Id) {
			affected = true
			continue
		}
		// Another enabled administrator remains.
		return nil
	}
	if !affected {
		// The edit touches no enabled administrator.
		return nil
	}
	return xerr.NewErrCodeMsg(xerr.InvalidParams, "the last enabled administrator cannot be demoted, disabled or deleted")
}

// isEnabledAdministrator reports whether u is an enabled administrator.
func isEnabledAdministrator(u *user.User) bool {
	return u != nil && u.IsAdmin != nil && *u.IsAdmin && u.Enable != nil && *u.Enable
}

// maxReferralPercentage is the whole of a purchase; a larger share cannot be
// paid.
const maxReferralPercentage = 100

// validateReferralPercentage refuses a referral share above the whole.
func validateReferralPercentage(percentage uint8) error {
	if percentage > maxReferralPercentage {
		return xerr.NewErrCodeMsg(xerr.InvalidParams, "the referral percentage cannot exceed 100")
	}
	return nil
}
