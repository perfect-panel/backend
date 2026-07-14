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

type GetInviteConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetInviteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetInviteConfigLogic {
	return &GetInviteConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetInviteConfigLogic) GetInviteConfig() (*dto.InviteConfig, error) {
	resp := &dto.InviteConfig{}
	// get invite config from db
	configs, err := l.svcCtx.Store.System().GetInviteConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetInviteConfigLogic] get invite config error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get invite config error: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)

	return resp, nil
}
