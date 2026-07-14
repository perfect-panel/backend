package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type UpdateCurrencyConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Currency Config
func NewUpdateCurrencyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCurrencyConfigLogic {
	return &UpdateCurrencyConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateCurrencyConfigLogic) UpdateCurrencyConfig(req *dto.CurrencyConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "currency", convertedConfigFields(*req), config.CurrencyConfigKey, config.GlobalConfigKey)
	initialize.Currency(l.svcCtx)
	if err != nil {
		l.Errorw("[UpdateCurrencyConfig] update currency config error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update invite config error: %v", err)
	}
	return nil
}
