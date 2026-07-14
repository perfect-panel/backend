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

type QueryUserCommissionLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query User Commission Log
func NewQueryUserCommissionLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserCommissionLogLogic {
	return &QueryUserCommissionLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserCommissionLogLogic) QueryUserCommissionLog(req *dto.QueryUserCommissionLogListRequest) (resp *dto.QueryUserCommissionLogListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeCommission.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		l.Errorw("Query User Commission Log failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Commission Log failed: %v", err)
	}
	var list []dto.CommissionLog

	for _, datum := range data {
		var content log.Commission
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Errorf("unmarshal commission log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.CommissionLog{
			UserId:    datum.ObjectID,
			Type:      content.Type,
			Amount:    content.Amount,
			OrderNo:   content.OrderNo,
			Timestamp: content.Timestamp,
		})
	}

	return &dto.QueryUserCommissionLogListResponse{
		List:  list,
		Total: total,
	}, nil
}
