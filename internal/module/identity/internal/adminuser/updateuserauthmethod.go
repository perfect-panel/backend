package adminuser

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserAuthMethod changes the identifier of an account's identity of
// the given type. Emails and phone numbers are stored normalized, and one
// that cannot be is refused.
func (s *Service) UpdateUserAuthMethod(ctx context.Context, req *dto.UpdateUserAuthMethodRequest) error {
	if strings.EqualFold(strings.TrimSpace(req.AuthType), "device") {
		return xerr.Errorf(xerr.InvalidParams, "use device management for device identities")
	}
	method, err := s.deps.UserAuths.FindUserAuthMethodByPlatform(ctx, req.UserId, req.AuthType)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity of user %d", req.AuthType, req.UserId)
	}
	userInfo, err := s.deps.Users.FindOne(ctx, req.UserId)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", req.UserId)
	}

	method.AuthType = req.AuthType
	method.AuthIdentifier = req.AuthIdentifier
	if err = s.deps.UserAuths.UpdateUserAuthMethods(ctx, method); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update %s identity of user %d", req.AuthType, req.UserId)
	}
	if err = s.deps.Cache.ClearUserCache(ctx, userInfo); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "clear cache of user %d", req.UserId)
	}
	return nil
}
