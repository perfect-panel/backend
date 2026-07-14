package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserNotifySettingLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserNotifySettingLogic Update user notify setting
func NewUpdateUserNotifySettingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserNotifySettingLogic {
	return &UpdateUserNotifySettingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserNotifySettingLogic) UpdateUserNotifySetting(req *dto.UpdateUserNotifySettingRequest) error {
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("[UpdateUserNotifySettingLogic] Find User Error:", logger.Field("err", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find User Error")
	}
	tool.DeepCopy(userInfo, req)
	err = l.svcCtx.Store.User().Update(l.ctx, userInfo)
	if err != nil {
		l.Errorw("[UpdateUserNotifySettingLogic] Update User Error:", logger.Field("err", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update User Error")
	}
	return nil
}
