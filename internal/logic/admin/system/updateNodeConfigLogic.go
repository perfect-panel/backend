package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
)

type UpdateNodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateNodeConfigLogic {
	return &UpdateNodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateNodeConfigLogic) UpdateNodeConfig(req *dto.NodeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "server", convertedConfigFields(*req))
	if err != nil {
		l.Errorw("[UpdateNodeConfig] update node config error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server config error: %v", err)
	}
	initialize.Node(l.svcCtx)
	return nil
}
