package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteSubscribeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete subscribe group
func NewDeleteSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteSubscribeGroupLogic {
	return &DeleteSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteSubscribeGroupLogic) DeleteSubscribeGroup(req *dto.DeleteSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().DeleteGroup(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[DeleteSubscribeGroupLogic] delete subscribe group failed: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete subscribe group failed: %v", err.Error())
	}
	return nil
}
