package log

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterEmailLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterEmailLogLogic Filter email log
func NewFilterEmailLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterEmailLogLogic {
	return &FilterEmailLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterEmailLogLogic) FilterEmailLog(req *dto.FilterLogParams) (resp *dto.FilterEmailLogResponse, err error) {
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:   req.Page,
		Size:   req.Size,
		Type:   log.TypeEmailMessage.Uint8(),
		Data:   req.Date,
		Search: req.Search,
	})

	if err != nil {
		l.Errorf("[FilterEmailLog] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}

	var list []dto.MessageLog

	for _, datum := range data {
		var content log.Message
		err = content.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Errorf("[FilterEmailLog] failed to unmarshal content: %v", err.Error())
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

	return &dto.FilterEmailLogResponse{
		Total: total,
		List:  list,
	}, nil
}
