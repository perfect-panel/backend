package plan

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BatchDeleteSubscribeGroupLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Batch delete subscribe group
func newBatchDeleteSubscribeGroupLogic(ctx context.Context, deps Deps) *BatchDeleteSubscribeGroupLogic {
	return &BatchDeleteSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *BatchDeleteSubscribeGroupLogic) BatchDeleteSubscribeGroup(req *dto.BatchDeleteSubscribeGroupRequest) error {
	err := l.deps.Plans.BatchDeleteGroup(l.ctx, req.Ids)
	if err != nil {
		l.Logger.Error("[BatchDeleteSubscribeGroup] Delete Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete subscribe group failed: %v", err.Error())
	}
	return nil
}
