package profile

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Logout ends only the calling session; a password change ends them all.
func (s *Service) Logout(ctx context.Context) error {
	sessionID, _ := ctx.Value(requestctx.CtxKeySessionID).(string)
	if sessionID == "" {
		return xerr.Errorf(xerr.InvalidAccess, "no session to end")
	}
	if err := usersession.End(ctx, s.deps.Redis, sessionID); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "end session")
	}
	return nil
}
