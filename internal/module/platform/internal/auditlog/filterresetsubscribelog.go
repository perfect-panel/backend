package auditlog

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterResetSubscribeLogLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewFilterResetSubscribeLogLogic Filter reset subscribe log
func newFilterResetSubscribeLogLogic(ctx context.Context, deps Deps) *FilterResetSubscribeLogLogic {
	return &FilterResetSubscribeLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *FilterResetSubscribeLogLogic) FilterResetSubscribeLog(req *dto.FilterResetSubscribeLogRequest) (resp *dto.FilterResetSubscribeLogResponse, err error) {
	data, total, err := l.deps.Logs.FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeResetSubscribe.Uint8(),
		ObjectID: req.UserSubscribeId,
		Data:     req.Date,
		Search:   req.Search,
	})

	if err != nil {
		l.Errorf("[FilterResetSubscribeLog] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}

	var list []dto.ResetSubscribeLog

	for _, item := range data {
		var content log.ResetSubscribe
		err = content.Unmarshal([]byte(item.Content))
		if err != nil {
			l.Errorf("[FilterResetSubscribeLog] failed to unmarshal content: %v", err.Error())
			continue
		}
		list = append(list, dto.ResetSubscribeLog{
			Type:            content.Type,
			UserId:          content.UserId,
			UserSubscribeId: item.ObjectID,
			OrderNo:         content.OrderNo,
			Timestamp:       content.Timestamp,
		})
	}

	return &dto.FilterResetSubscribeLogResponse{
		List:  list,
		Total: total,
	}, nil
}
