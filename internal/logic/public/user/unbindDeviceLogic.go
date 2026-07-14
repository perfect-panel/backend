package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UnbindDeviceLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Unbind Device
func NewUnbindDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbindDeviceLogic {
	return &UnbindDeviceLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbindDeviceLogic) UnbindDevice(req *dto.UnbindDeviceRequest) error {
	userInfo := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	device, err := l.svcCtx.Store.User().FindOneDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DeviceNotExist), "find device")
	}

	if device.UserId != userInfo.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "device not belong to user")
	}

	return l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if err = store.User().DeleteDevice(l.ctx, req.Id); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete device err: %v", err)
		}

		if err = store.User().DeleteUserAuthMethodByIdentifier(l.ctx, "device", device.Identifier); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find device online record err: %v", err)
		}
		sessionId := l.ctx.Value(constant.CtxKeySessionID)
		sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
		l.svcCtx.Redis.Del(l.ctx, sessionIdCacheKey)
		return nil
	})
}
