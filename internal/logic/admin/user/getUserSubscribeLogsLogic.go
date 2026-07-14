package user

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserSubscribeLogsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe logs
func NewGetUserSubscribeLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeLogsLogic {
	return &GetUserSubscribeLogsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeLogsLogic) GetUserSubscribeLogs(req *dto.GetUserSubscribeLogsRequest) (resp *dto.GetUserSubscribeLogsResponse, err error) {
	params := &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: req.UserId,
	}
	if req.SubscribeId != 0 {
		params.Search = `"user_subscribe_id":` + strconv.FormatInt(req.SubscribeId, 10)
	}

	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, params)

	if err != nil {
		l.Errorw("[GetUserSubscribeLogs] Get User Subscribe Logs Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Subscribe Logs Error")
	}
	var list []dto.UserSubscribeLog

	for _, datum := range data {
		var content log.Subscribe
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Errorf("[GetUserSubscribeLogs] unmarshal subscribe log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.UserSubscribeLog{
			Id:              datum.Id,
			UserId:          datum.ObjectID,
			UserSubscribeId: content.UserSubscribeId,
			Token:           content.Token,
			IP:              content.ClientIP,
			UserAgent:       content.UserAgent,
			Timestamp:       datum.CreatedAt.UnixMilli(),
		})
	}

	return &dto.GetUserSubscribeLogsResponse{
		List:  list,
		Total: total,
	}, err
}
