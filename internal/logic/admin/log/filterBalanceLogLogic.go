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

type FilterBalanceLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterBalanceLogLogic Filter balance log
func NewFilterBalanceLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterBalanceLogLogic {
	return &FilterBalanceLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterBalanceLogLogic) FilterBalanceLog(req *dto.FilterBalanceLogRequest) (resp *dto.FilterBalanceLogResponse, err error) {
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeBalance.Uint8(),
		Data:     req.Date,
		ObjectID: req.UserId,
	})

	if err != nil {
		l.Errorw("[FilterBalanceLog] Query User Balance Log Error:", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Balance Log Error")
	}

	list := make([]dto.BalanceLog, 0)
	for _, datum := range data {
		var content log.Balance
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Errorf("[QueryUserBalanceLog] unmarshal balance log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.BalanceLog{
			UserId:    datum.ObjectID,
			Amount:    content.Amount,
			Type:      content.Type,
			OrderNo:   content.OrderNo,
			Balance:   content.Balance,
			Timestamp: content.Timestamp,
		})
	}

	return &dto.FilterBalanceLogResponse{
		Total: total,
		List:  list,
	}, nil
}
