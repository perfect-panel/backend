package system

import (
	"context"
	"os"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetModuleConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Module Config
func NewGetModuleConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleConfigLogic {
	return &GetModuleConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleConfigLogic) GetModuleConfig() (resp *dto.ModuleConfig, err error) {
	value, exists := os.LookupEnv("SECRET_KEY")
	if !exists {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), " SECRET_KEY not set in environment variables")
	}

	return &dto.ModuleConfig{
		Secret:         value,
		ServiceName:    constant.ServiceName,
		ServiceVersion: constant.Version,
	}, nil
}
