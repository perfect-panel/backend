package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type KickOfflineByUserDeviceLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// kick offline user device
func NewKickOfflineByUserDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickOfflineByUserDeviceLogic {
	return &KickOfflineByUserDeviceLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *KickOfflineByUserDeviceLogic) KickOfflineByUserDevice(req *dto.KickOfflineRequest) error {
	device, err := l.svcCtx.Store.UserDevice().FindOneDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get Device  error: %v", err.Error())
	}
	l.svcCtx.DeviceManager.KickDevice(device.UserId, device.Identifier)
	device.Online = false
	err = l.svcCtx.Store.UserDevice().UpdateDevice(l.ctx, device)
	if err != nil {
		l.Logger.Error("[KickOfflineByUserDeviceLogic] Update Device Error:", logger.Field("err", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update Device error: %v", err.Error())
	}

	return nil
}
