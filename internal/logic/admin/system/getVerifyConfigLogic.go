package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetVerifyConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetVerifyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetVerifyConfigLogic {
	return &GetVerifyConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetVerifyConfigLogic) GetVerifyConfig() (*dto.VerifyConfig, error) {
	resp := &dto.VerifyConfig{}
	// get verify config from db
	verifyConfigs, err := l.svcCtx.Store.System().GetVerifyConfig(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get verify config failed: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(verifyConfigs, resp)
	// update verify config to system
	initialize.Verify(l.svcCtx)
	return resp, nil
}
