package adminuser

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/devicestate"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeleteUserDevice removes a device with its identity and sessions, and
// disconnects it.
func (s *Service) DeleteUserDevice(ctx context.Context, req *dto.DeleteUserDeviceRequest) error {
	device, err := devicestate.Delete(ctx, s.deps.Store, s.deps.Redis, req.Id, 0)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete device %d", req.Id)
	}
	if device != nil {
		s.deps.kickDevice(device.UserId, device.Identifier)
	}
	return nil
}
