package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/devicesession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserDevice enables or disables a device. Its sessions end either
// way, and a disabled device is disconnected.
func (s *Service) UpdateUserDevice(ctx context.Context, req *dto.UserDevice) error {
	device, err := s.deps.Devices.FindDeviceForAuth(ctx, req.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "get Device  error: %v", err.Error())
	}
	if err := devicesession.Revoke(ctx, s.deps.Redis, device.Id); err != nil {
		return err
	}
	if err := s.deps.Devices.SetDeviceEnabled(ctx, device.Id, req.Enabled); err != nil {
		logger.WithContext(ctx).Error("[UpdateUserDevice] Update Device Error:", logger.Field("err", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update Device error: %v", err.Error())
	}
	if !req.Enabled {
		s.deps.kickDevice(device.UserId, device.Identifier)
	}
	return nil
}
