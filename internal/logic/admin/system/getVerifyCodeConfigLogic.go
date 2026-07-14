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

type GetVerifyCodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Verify Code Config
func NewGetVerifyCodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetVerifyCodeConfigLogic {
	return &GetVerifyCodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetVerifyCodeConfigLogic) GetVerifyCodeConfig() (resp *dto.VerifyCodeConfig, err error) {
	data, err := l.svcCtx.Store.System().GetVerifyCodeConfig(l.ctx)
	if err != nil {
		l.Errorw("Get Verify Code Config Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get Verify Code Config Error: %s", err.Error())
	}
	resp = &dto.VerifyCodeConfig{}
	tool.SystemConfigSliceReflectToStruct(data, resp)
	return
}
