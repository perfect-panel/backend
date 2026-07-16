package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type UpdateVerifyCodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Verify Code Config
func NewUpdateVerifyCodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateVerifyCodeConfigLogic {
	return &UpdateVerifyCodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateVerifyCodeConfigLogic) UpdateVerifyCodeConfig(req *dto.VerifyCodeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "verify_code", convertedConfigFields(*req), config.VerifyCodeConfigKey)
	if err != nil {
		l.Errorw("[UpdateRegisterConfig] update verify code config error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update register config error: %v", err.Error())
	}
	initialize.Verify(l.svcCtx)
	return nil
}
