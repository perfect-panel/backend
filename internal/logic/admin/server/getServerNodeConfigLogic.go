package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/nodeconfig"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetServerNodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetServerNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetServerNodeConfigLogic {
	return &GetServerNodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerNodeConfigLogic) GetServerNodeConfig(req *dto.GetServerNodeConfigRequest) (*dto.GetServerNodeConfigResponse, error) {
	nodeStore := l.svcCtx.Store.Node()
	if _, err := nodeStore.FindOneServer(l.ctx, req.ServerID); err != nil {
		l.Errorf("[GetServerNodeConfig] FindOneServer Error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server error: %v", err)
	}

	override, err := nodeStore.FindServerConfigOverride(l.ctx, req.ServerID)
	if err != nil {
		l.Errorf("[GetServerNodeConfig] FindServerConfigOverride Error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server node config error: %v", err)
	}

	global := nodeconfig.GlobalValues(l.svcCtx.Config.Node)
	effective := nodeconfig.CloneValues(global)
	if err := nodeconfig.ApplyOverride(&effective, override); err != nil {
		l.Errorf("[GetServerNodeConfig] ApplyOverride Error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "apply server node config override error: %v", err)
	}
	overrideResp, err := nodeconfig.OverrideResponse(override)
	if err != nil {
		l.Errorf("[GetServerNodeConfig] OverrideResponse Error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse server node config override error: %v", err)
	}

	return &dto.GetServerNodeConfigResponse{
		Global:    global,
		Override:  overrideResp,
		Effective: effective,
	}, nil
}
