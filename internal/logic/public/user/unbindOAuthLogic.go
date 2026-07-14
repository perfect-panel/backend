package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UnbindOAuthLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Unbind OAuth
func NewUnbindOAuthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbindOAuthLogic {
	return &UnbindOAuthLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbindOAuthLogic) UnbindOAuth(req *dto.UnbindOAuthRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if !l.validator(req) {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid parameter")
	}
	err := l.svcCtx.Store.User().DeleteUserAuthMethods(l.ctx, u.Id, req.Method)
	if err != nil {
		l.Errorw("delete user auth methods failed:", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete user auth methods failed: %v", err.Error())
	}

	return nil
}
func (l *UnbindOAuthLogic) validator(req *dto.UnbindOAuthRequest) bool {
	return req.Method != "" && req.Method != "email" && req.Method != "mobile"
}
