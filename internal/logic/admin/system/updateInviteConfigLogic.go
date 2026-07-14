package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
)

type UpdateInviteConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateInviteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateInviteConfigLogic {
	return &UpdateInviteConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateInviteConfigLogic) UpdateInviteConfig(req *dto.InviteConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "invite", convertedConfigFields(*req), config.InviteConfigKey, config.GlobalConfigKey)
	if err != nil {
		l.Errorw("[UpdateInviteConfig] update invite config error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update invite config error: %v", err)
	}
	initialize.Invite(l.svcCtx)
	return nil
}
