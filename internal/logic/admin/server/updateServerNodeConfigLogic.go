package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/nodeconfig"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateServerNodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateServerNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateServerNodeConfigLogic {
	return &UpdateServerNodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateServerNodeConfigLogic) UpdateServerNodeConfig(req *dto.UpdateServerNodeConfigRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	if _, err := nodeStore.FindOneServer(l.ctx, req.ServerID); err != nil {
		l.Errorf("[UpdateServerNodeConfig] FindOneServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server error: %v", err)
	}

	data, allInherited, err := nodeconfig.OverrideModel(req.ServerID, req.ServerNodeConfigOverride)
	if err != nil {
		l.Errorf("[UpdateServerNodeConfig] OverrideModel Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "server node config is invalid"), "server node config is invalid: %v", err)
	}

	if allInherited {
		err = nodeStore.DeleteServerConfigOverride(l.ctx, req.ServerID)
	} else {
		err = nodeStore.SaveServerConfigOverride(l.ctx, data)
	}
	if err != nil {
		l.Errorf("[UpdateServerNodeConfig] SaveServerConfigOverride Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server node config error: %v", err)
	}

	return nodeStore.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{req.ServerID},
	})
}
