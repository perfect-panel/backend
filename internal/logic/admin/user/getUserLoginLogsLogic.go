package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserLoginLogsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user login logs
func NewGetUserLoginLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLoginLogsLogic {
	return &GetUserLoginLogsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserLoginLogsLogic) GetUserLoginLogs(req *dto.GetUserLoginLogsRequest) (resp *dto.GetUserLoginLogsResponse, err error) {
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: req.UserId,
	})
	if err != nil {
		l.Errorw("[GetUserLoginLogs] get user login logs failed", logger.Field("error", err.Error()), logger.Field("request", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get user login logs failed: %v", err.Error())
	}
	var list []dto.UserLoginLog

	for _, datum := range data {
		var content log.Login
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Errorf("[GetUserLoginLogs] unmarshal login log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.UserLoginLog{
			Id:        datum.Id,
			UserId:    datum.ObjectID,
			LoginIP:   content.LoginIP,
			UserAgent: content.UserAgent,
			Success:   content.Success,
			Timestamp: datum.CreatedAt.UnixMilli(),
		})
	}

	return &dto.GetUserLoginLogsResponse{
		Total: total,
		List:  list,
	}, nil
}
