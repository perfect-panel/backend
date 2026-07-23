package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserAuthMethodLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update user auth method
func NewUpdateUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserAuthMethodLogic {
	return &UpdateUserAuthMethodLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserAuthMethodLogic) UpdateUserAuthMethod(req *dto.UpdateUserAuthMethodRequest) error {
	method, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByPlatform(l.ctx, req.UserId, req.AuthType)
	if err != nil {
		l.Errorw("Get user auth method error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId), logger.Field("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get user auth method error: %v", err.Error())
	}
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("Get user info error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get user info error: %v", err.Error())
	}

	method.AuthType = req.AuthType
	method.AuthIdentifier = req.AuthIdentifier
	if err = l.svcCtx.Store.UserAuth().UpdateUserAuthMethods(l.ctx, method); err != nil {
		l.Errorw("Update user auth method error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId), logger.Field("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update user auth method error: %v", err.Error())
	}
	if err = l.svcCtx.Store.UserCache().UpdateUserCache(l.ctx, userInfo); err != nil {
		l.Errorw("Update user cache error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Update user cache error: %v", err.Error())
	}
	return nil
}
