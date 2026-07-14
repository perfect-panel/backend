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

type GetSiteConfigLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logger.Logger
}

func NewGetSiteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSiteConfigLogic {
	return &GetSiteConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSiteConfigLogic) GetSiteConfig() (resp *dto.SiteConfig, err error) {
	resp = &dto.SiteConfig{}
	// get site config from db
	siteConfigs, err := l.svcCtx.Store.System().GetSiteConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetSiteConfig] Database query error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get site config failed: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(siteConfigs, resp)
	return resp, nil
}
