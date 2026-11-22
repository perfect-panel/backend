package profile

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/devicestate"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UnbindDevice removes one of the calling account's devices with its
// identity and sessions, and disconnects it.
func (s *Service) UnbindDevice(ctx context.Context, req *dto.UnbindDeviceRequest) error {
	userInfo, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	device, err := s.deps.Devices.FindDeviceForAuth(ctx, req.Id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Errorf(xerr.DeviceNotExist, "device %d does not exist", req.Id)
	}
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find device %d", req.Id)
	}

	if device.UserId != userInfo.Id {
		return xerr.Errorf(xerr.InvalidParams, "device does not belong to the user")
	}

	removed, err := devicestate.Delete(ctx, s.deps.Store, s.deps.Redis, req.Id, userInfo.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "remove device %d", req.Id)
	}
	if removed != nil && s.deps.KickDevice != nil {
		s.deps.KickDevice(removed.UserId, removed.Identifier)
	}
	return nil
}
