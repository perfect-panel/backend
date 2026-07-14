package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateSubscribeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSubscribeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeConfigLogic {
	return &UpdateSubscribeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeConfigLogic) UpdateSubscribeConfig(req *dto.SubscribeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "subscribe", convertedConfigFields(*req), config.SubscribeConfigKey, config.GlobalConfigKey)

	if err != nil {
		l.Errorw("[UpdateSubscribeConfigLogic] update subscribe config error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe config error: %v", err)
	}

	if l.svcCtx.Config.Subscribe.SubscribePath != req.SubscribePath {
		go func(svc *svc.ServiceContext) {
			err = svc.Restart()
			if err != nil {
				l.Errorw("[UpdateSubscribeConfigLogic] restart error: ", logger.Field("error", err.Error()))
			}
		}(l.svcCtx)
		return nil
	}

	initialize.Subscribe(l.svcCtx)
	return nil
}
