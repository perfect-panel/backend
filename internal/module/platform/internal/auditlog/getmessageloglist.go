package auditlog

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetMessageLogListLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewGetMessageLogListLogic Get message log list
func newGetMessageLogListLogic(ctx context.Context, deps Deps) *GetMessageLogListLogic {
	return &GetMessageLogListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *GetMessageLogListLogic) GetMessageLogList(req *dto.GetMessageLogListRequest) (resp *dto.GetMessageLogListResponse, err error) {

	data, total, err := l.deps.Logs.FilterSystemLog(l.ctx, &log.FilterParams{
		Page:   req.Page,
		Size:   req.Size,
		Type:   req.Type,
		Search: req.Search,
	})

	if err != nil {
		l.Errorf("[GetMessageLogList] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}

	var list []dto.MessageLog

	for _, datum := range data {
		var content log.Message
		err = content.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Errorf("[GetMessageLogList] failed to unmarshal content: %v", err.Error())
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

	return &dto.GetMessageLogListResponse{
		Total: total,
		List:  list,
	}, nil
}
