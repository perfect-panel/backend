package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BatchDeleteSubscribeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete subscribe group
func NewBatchDeleteSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteSubscribeGroupLogic {
	return &BatchDeleteSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteSubscribeGroupLogic) BatchDeleteSubscribeGroup(req *dto.BatchDeleteSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().BatchDeleteGroup(l.ctx, req.Ids)
	if err != nil {
		l.Logger.Error("[BatchDeleteSubscribeGroup] Delete Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete subscribe group failed: %v", err.Error())
	}
	return nil
}
