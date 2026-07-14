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

type GetRegisterConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRegisterConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRegisterConfigLogic {
	return &GetRegisterConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRegisterConfigLogic) GetRegisterConfig() (*dto.RegisterConfig, error) {
	resp := &dto.RegisterConfig{}

	// get register config from database
	configs, err := l.svcCtx.Store.System().GetRegisterConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetRegisterConfig] Database query error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get register config error: %v", err.Error())
	}

	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
