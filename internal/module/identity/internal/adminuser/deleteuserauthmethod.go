package adminuser

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteUserAuthMethod removes the account's identity of the given type.
// Device identities are removed with their device.
func (s *Service) DeleteUserAuthMethod(ctx context.Context, req *dto.DeleteUserAuthMethodRequest) error {
	if strings.EqualFold(strings.TrimSpace(req.AuthType), "device") {
		return xerr.Errorf(xerr.InvalidParams, "use device management for device identities")
	}
	if err := s.deps.UserAuths.DeleteUserAuthMethods(ctx, req.UserId, req.AuthType); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteUserAuthMethod] delete user auth method failed", logger.Field("err", err.Error()), logger.Field("userId", req.UserId), logger.Field("authType", req.AuthType))
		return xerr.Errorf(xerr.DatabaseDeletedError, "Delete User Auth Method Error")
	}
	return nil
}
