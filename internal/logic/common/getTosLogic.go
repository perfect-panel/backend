package common

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetTosLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Tos
func NewGetTosLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTosLogic {
	return &GetTosLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTosLogic) GetTos() (resp *dto.GetTosResponse, err error) {
	resp = &dto.GetTosResponse{}
	// get Tos config from db
	configs, err := l.svcCtx.Store.System().GetTosConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetTosLogic] GetTos error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetTos error: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
