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

type FilterLoginLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterLoginLogLogic Filter login log
func NewFilterLoginLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterLoginLogLogic {
	return &FilterLoginLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterLoginLogLogic) FilterLoginLog(req *dto.FilterLoginLogRequest) (resp *dto.FilterLoginLogResponse, err error) {
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: req.UserId,
		Data:     req.Date,
		Search:   req.Search,
	})

	if err != nil {
		l.Errorf("[FilterLoginLog] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}
	var list []dto.LoginLog
	for _, datum := range data {
		var item log.Login
		err = item.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Errorf("[FilterLoginLog] failed to unmarshal content: %v", err.Error())
			continue
		}
		list = append(list, dto.LoginLog{
			UserId:    datum.ObjectID,
			Method:    item.Method,
			LoginIP:   item.LoginIP,
			UserAgent: item.UserAgent,
			Success:   item.Success,
			Timestamp: datum.CreatedAt.UnixMilli(),
		})
	}

	return &dto.FilterLoginLogResponse{
		Total: total,
		List:  list,
	}, nil
}
