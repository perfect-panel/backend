package profile

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetOAuthMethods lists the calling account's identities, their identifiers
// masked like the account view's: a device identifier signs the device in,
// so a web session must not read it back in full.
func (s *Service) GetOAuthMethods(ctx context.Context) (*dto.GetOAuthMethodsResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	methods, err := s.deps.UserAuth.FindUserAuthMethods(ctx, u.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("find user auth methods failed:", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user auth methods failed: %v", err.Error())
	}
	return &dto.GetOAuthMethodsResponse{
		Methods: maskAuthMethods(methods),
	}, nil
}
