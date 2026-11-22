package profile

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UnbindOAuth removes the calling account's binding of a third-party
// provider.
func (s *Service) UnbindOAuth(ctx context.Context, req *dto.UnbindOAuthRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if !isOAuthMethod(req.Method) {
		return xerr.Errorf(xerr.InvalidParams, "invalid parameter")
	}
	if err := s.deps.UserAuth.DeleteUserAuthMethods(ctx, u.Id, req.Method); err != nil {
		logger.WithContext(ctx).Errorw("delete user auth methods failed:", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete user auth methods failed: %v", err.Error())
	}
	return nil
}

// isOAuthMethod reports whether method names a third-party provider, the
// only bindings an account may drop by itself.
func isOAuthMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "google", "apple", "github", "facebook", "telegram":
		return true
	default:
		return false
	}
}
