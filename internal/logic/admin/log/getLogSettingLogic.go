package log

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type GetLogSettingLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get log setting
func NewGetLogSettingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLogSettingLogic {
	return &GetLogSettingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetLogSettingLogic) GetLogSetting() (resp *dto.LogSetting, err error) {
	configs, err := l.svcCtx.Store.System().GetLogConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetLogSetting] Database query error", logger.Field("error", err.Error()))
		return nil, err
	}
	resp = &dto.LogSetting{}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
