package auditlog

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterMobileLogLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Filter mobile log
func newFilterMobileLogLogic(ctx context.Context, deps Deps) *FilterMobileLogLogic {
	return &FilterMobileLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *FilterMobileLogLogic) FilterMobileLog(req *dto.FilterLogParams) (resp *dto.FilterMobileLogResponse, err error) {
	data, total, err := l.deps.Logs.FilterSystemLog(l.ctx, &log.FilterParams{
		Page:   req.Page,
		Size:   req.Size,
		Type:   log.TypeMobileMessage.Uint8(),
		Data:   req.Date,
		Search: req.Search,
	})

	if err != nil {
		l.Errorf("[FilterMobileLog] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}

	var list []dto.MessageLog

	for _, datum := range data {
		var content log.Message
		err = content.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Errorf("[FilterMobileLog] failed to unmarshal content: %v", err.Error())
			continue
		}
		list = append(list, dto.MessageLog{
			Id:        datum.Id,
			Type:      datum.Type,
			Platform:  content.Platform,
			To:        content.To,
			Subject:   content.Subject,
			Content:   content.Content,
			Status:    content.Status,
			CreatedAt: datum.CreatedAt.UnixMilli(),
		})
	}

	return &dto.FilterMobileLogResponse{
		Total: total,
		List:  list,
	}, nil
}
