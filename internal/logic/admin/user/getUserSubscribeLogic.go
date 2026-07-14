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

type GetUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe
func NewGetUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeLogic {
	return &GetUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeLogic) GetUserSubscribe(req *dto.GetUserSubscribeListRequest) (resp *dto.GetUserSubscribeListResponse, err error) {
	data, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, req.UserId, 0, 1, 2, 3, 4, 5)
	if err != nil {
		l.Errorw("[GetUserSubscribeLogs] Get User Subscribe Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Subscribe Error")
	}

	resp = &dto.GetUserSubscribeListResponse{
		List:  make([]dto.UserSubscribe, 0),
		Total: int64(len(data)),
	}

	for _, item := range data {
		var sub dto.UserSubscribe
		tool.DeepCopy(&sub, item)
		sub.Short, _ = tool.FixedUniqueString(item.Token, 8, "")
		resp.List = append(resp.List, sub)
	}
	return
}
