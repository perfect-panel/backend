package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/devicesession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// KickOfflineByUserDevice ends a device's sessions, disconnects it and shows
// it offline; the device stays bound.
func (s *Service) KickOfflineByUserDevice(ctx context.Context, req *dto.KickOfflineRequest) error {
	device, err := s.deps.Devices.FindDeviceForAuth(ctx, req.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "get Device  error: %v", err.Error())
	}
	if err := devicesession.Revoke(ctx, s.deps.Redis, device.Id); err != nil {
		return err
	}
	s.deps.kickDevice(device.UserId, device.Identifier)
	if err := s.deps.Devices.SetDeviceOnline(ctx, device.Id, false); err != nil {
		logger.WithContext(ctx).Error("[KickOfflineByUserDevice] Update Device Error:", logger.Field("err", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update Device error: %v", err.Error())
	}
	return nil
}
