package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetUserSubscribeTrafficLogsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe traffic logs
func NewGetUserSubscribeTrafficLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeTrafficLogsLogic {
	return &GetUserSubscribeTrafficLogsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeTrafficLogsLogic) GetUserSubscribeTrafficLogs(req *dto.GetUserSubscribeTrafficLogsRequest) (resp *dto.GetUserSubscribeTrafficLogsResponse, err error) {
	list, total, err := l.svcCtx.Store.TrafficLog().QueryTrafficLogPageList(l.ctx, req.UserId, req.SubscribeId, req.Page, req.Size)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetUserSubscribeTrafficLogs failed: %v", err.Error())
	}
	userRespList := make([]dto.TrafficLog, 0)
	tool.DeepCopy(&userRespList, list)
	return &dto.GetUserSubscribeTrafficLogsResponse{
		Total: total,
		List:  userRespList,
	}, nil
}
