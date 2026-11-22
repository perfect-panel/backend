package profile

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetDeviceList lists the calling account's devices, their identifiers
// masked: an identifier signs the device in, so a web session must not read
// it back in full.
func (s *Service) GetDeviceList(ctx context.Context) (*dto.GetDeviceListResponse, error) {
	userInfo, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	list, count, err := s.deps.Devices.QueryDeviceList(ctx, userInfo.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list devices of user %d", userInfo.Id)
	}
	userRespList := make([]dto.UserDevice, 0)
	if err := mapping.Copy(&userRespList, list); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map devices of user %d", userInfo.Id)
	}
	return &dto.GetDeviceListResponse{
		Total: count,
		List:  maskDevices(userRespList),
	}, nil
}
