package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateSubscribeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update subscribe group
func NewUpdateSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeGroupLogic {
	return &UpdateSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeGroupLogic) UpdateSubscribeGroup(req *dto.UpdateSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().UpdateGroup(l.ctx, &subscribe.Group{
		Id:          req.Id,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		l.Logger.Error("[UpdateSubscribeGroup] update subscribe group failed", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe group failed: %v", err.Error())
	}
	return nil
}
