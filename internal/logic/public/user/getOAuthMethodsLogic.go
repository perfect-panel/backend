package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetOAuthMethodsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get OAuth Methods
func NewGetOAuthMethodsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOAuthMethodsLogic {
	return &GetOAuthMethodsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetOAuthMethodsLogic) GetOAuthMethods() (resp *dto.GetOAuthMethodsResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	methods, err := l.svcCtx.Store.UserAuth().FindUserAuthMethods(l.ctx, u.Id)
	if err != nil {
		l.Errorw("find user auth methods failed:", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth methods failed: %v", err.Error())
	}
	list := make([]dto.UserAuthMethod, 0)
	tool.DeepCopy(&list, methods)
	return &dto.GetOAuthMethodsResponse{
		Methods: list,
	}, nil
}
