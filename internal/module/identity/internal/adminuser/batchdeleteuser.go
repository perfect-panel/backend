package adminuser

import (
	"context"
	"slices"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// BatchDeleteUser soft-deletes the accounts and drops the caches that keep
// serving them. The demo instance's administrator cannot be deleted, and
// neither can the last enabled administrator.
func (s *Service) BatchDeleteUser(ctx context.Context, req *dto.BatchDeleteUserRequest) error {
	if slices.Contains(req.Ids, demoAdminID) && demoMode() {
		return demoRestricted("delete the admin user")
	}
	if err := ensureAnotherAdministrator(ctx, s.deps.Users, req.Ids...); err != nil {
		return err
	}
	if err := s.deps.Users.BatchDeleteUser(ctx, req.Ids); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete users %v", req.Ids)
	}
	clearUserAccessCaches(ctx, s.deps, req.Ids)
	return nil
}
