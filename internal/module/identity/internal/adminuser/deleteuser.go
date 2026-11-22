package adminuser

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteUser soft-deletes the account and drops the caches that keep serving
// it. The demo instance's administrator cannot be deleted, and neither can
// the last enabled administrator.
func (s *Service) DeleteUser(ctx context.Context, req *dto.GetDetailRequest) error {
	if req.Id == demoAdminID && demoMode() {
		return demoRestricted("delete the admin user")
	}
	if err := ensureAnotherAdministrator(ctx, s.deps.Users, req.Id); err != nil {
		return err
	}
	if err := s.deps.Users.Delete(ctx, req.Id); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete user %d", req.Id)
	}
	clearUserAccessCaches(ctx, s.deps, []int64{req.Id})
	return nil
}
