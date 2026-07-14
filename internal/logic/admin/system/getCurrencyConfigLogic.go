package system

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetCurrencyConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Currency Config
func NewGetCurrencyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCurrencyConfigLogic {
	return &GetCurrencyConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetCurrencyConfigLogic) GetCurrencyConfig() (resp *dto.CurrencyConfig, err error) {
	configs, err := l.svcCtx.Store.System().GetCurrencyConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetCurrencyConfigLogic] GetCurrencyConfig error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetCurrencyConfig error: %v", err.Error())
	}
	resp = &dto.CurrencyConfig{}
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
