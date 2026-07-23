package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type DeleteUserDeviceLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete user device
func NewDeleteUserDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserDeviceLogic {
	return &DeleteUserDeviceLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserDeviceLogic) DeleteUserDevice(req *dto.DeleteUserDeivceRequest) error {
	err := l.svcCtx.Store.UserDevice().DeleteDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete user error: %v", err.Error())
	}
	return nil
}
