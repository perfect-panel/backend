package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"

	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
)

type UpdateRegisterConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateRegisterConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateRegisterConfigLogic {
	return &UpdateRegisterConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateRegisterConfigLogic) UpdateRegisterConfig(req *dto.RegisterConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "register", convertedConfigFields(*req), config.RegisterConfigKey, config.GlobalConfigKey)
	if err != nil {
		l.Errorw("[UpdateRegisterConfig] update register config error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update register config error: %v", err.Error())
	}
	// init system config
	initialize.Register(l.svcCtx)
	return nil
}
