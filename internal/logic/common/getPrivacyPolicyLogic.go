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

type GetPrivacyPolicyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Privacy Policy
func NewGetPrivacyPolicyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPrivacyPolicyLogic {
	return &GetPrivacyPolicyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPrivacyPolicyLogic) GetPrivacyPolicy() (resp *dto.PrivacyPolicyConfig, err error) {
	resp = &dto.PrivacyPolicyConfig{}
	// get tos config from db
	configs, err := l.svcCtx.Store.System().GetTosConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetTosConfig] GetTosConfig error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetTosConfig error: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
