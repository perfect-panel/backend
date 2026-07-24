package selfsub

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetSubscribeLogLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewGetSubscribeLogLogic Get Subscribe Log
func newGetSubscribeLogLogic(ctx context.Context, deps Deps) *GetSubscribeLogLogic {
	return &GetSubscribeLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *GetSubscribeLogLogic) GetSubscribeLog(req *dto.GetSubscribeLogRequest) (resp *dto.GetSubscribeLogResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	data, total, err := l.deps.Logs.FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: u.Id, // filter by current user id
	})
	if err != nil {
		l.Errorw("[GetUserSubscribeLogs] Get User Subscribe Logs Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Subscribe Logs Error")
	}
	var list []dto.UserSubscribeLog

	for _, item := range data {
		var content log.Subscribe
		if err = content.Unmarshal([]byte(item.Content)); err != nil {
			l.Errorf("[GetUserSubscribeLogs] unmarshal subscribe log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.UserSubscribeLog{
			Id:              item.Id,
			UserId:          item.ObjectID,
			UserSubscribeId: content.UserSubscribeId,
			Token:           content.Token,
			IP:              content.ClientIP,
			UserAgent:       content.UserAgent,
			Timestamp:       item.CreatedAt.UnixMilli(),
		})
	}

	return &dto.GetSubscribeLogResponse{
		List:  list,
		Total: total,
	}, err
}
