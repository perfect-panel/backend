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

type GetUserAuthMethodLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user auth method
func NewGetUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserAuthMethodLogic {
	return &GetUserAuthMethodLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserAuthMethodLogic) GetUserAuthMethod(req *dto.GetUserAuthMethodRequest) (resp *dto.GetUserAuthMethodResponse, err error) {
	methods, err := l.svcCtx.Store.UserAuth().FindUserAuthMethods(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("[GetUserAuthMethodLogic] Get User Auth Method Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Auth Method Error")
	}
	list := make([]dto.UserAuthMethod, 0)
	tool.DeepCopy(&list, methods)

	return &dto.GetUserAuthMethodResponse{
		AuthMethods: list,
	}, nil
}
