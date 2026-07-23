package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteUserAuthMethodLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete user auth method
func NewDeleteUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserAuthMethodLogic {
	return &DeleteUserAuthMethodLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserAuthMethodLogic) DeleteUserAuthMethod(req *dto.DeleteUserAuthMethodRequest) error {
	err := l.svcCtx.Store.UserAuth().DeleteUserAuthMethods(l.ctx, req.UserId, req.AuthType)
	if err != nil {
		l.Errorw("[DeleteUserAuthMethodLogic] Delete User Auth Method Error:", logger.Field("err", err.Error()), logger.Field("userId", req.UserId), logger.Field("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "Delete User Auth Method Error")
	}
	return nil
}
