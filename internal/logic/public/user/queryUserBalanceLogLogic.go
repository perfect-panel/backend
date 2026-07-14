package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryUserBalanceLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryUserBalanceLogLogic Query User Balance Log
func NewQueryUserBalanceLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserBalanceLogLogic {
	return &QueryUserBalanceLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserBalanceLogLogic) QueryUserBalanceLog() (resp *dto.QueryUserBalanceLogListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     1,
		Size:     100,
		Type:     log.TypeBalance.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		l.Errorw("[QueryUserBalanceLog] Query User Balance Log Error:", logger.Field("err", err.Error()))
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

	return &dto.QueryUserBalanceLogListResponse{
		Total: total,
		List:  list,
	}, nil
}
