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

type GetUserSubscribeByIdLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe by id
func NewGetUserSubscribeByIdLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeByIdLogic {
	return &GetUserSubscribeByIdLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeByIdLogic) GetUserSubscribeById(req *dto.GetUserSubscribeByIdRequest) (resp *dto.UserSubscribeDetail, err error) {
	sub, err := l.svcCtx.Store.User().FindOneSubscribeDetailsById(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[GetUserSubscribeByIdLogic] FindOneSubscribeDetailsById error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribeDetailsById error: %v", err.Error())
	}
	var subscribeDetails dto.UserSubscribeDetail
	tool.DeepCopy(&subscribeDetails, sub)
	return &subscribeDetails, nil
}
