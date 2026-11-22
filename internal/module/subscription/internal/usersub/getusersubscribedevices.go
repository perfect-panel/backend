package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribeDevices pages the user's devices, optionally only those of
// one subscription.
func (s *Service) GetUserSubscribeDevices(ctx context.Context, req *dto.GetUserSubscribeDevicesRequest) (*dto.GetUserSubscribeDevicesResponse, error) {
	list, total, err := s.deps.Devices.QueryDevicePageList(ctx, req.UserId, req.SubscribeId, req.Page, req.Size)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetUserSubscribeDevices failed: %v", err.Error())
	}
	devices := make([]dto.SubscriptionUserDeviceSnapshot, 0, len(list))
	for _, device := range list {
		devices = append(devices, dto.SubscriptionUserDeviceSnapshot{
			Id:         device.Id,
			Ip:         device.Ip,
			Identifier: device.Identifier,
			UserAgent:  device.UserAgent,
			Online:     device.Online,
			Enabled:    device.Enabled,
			CreatedAt:  device.CreatedAt.UnixMilli(),
			UpdatedAt:  device.UpdatedAt.UnixMilli(),
		})
	}
	return &dto.GetUserSubscribeDevicesResponse{
		Total: total,
		List:  devices,
	}, nil
}
