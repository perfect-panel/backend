package auditlog

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type GetLogSettingLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Get log setting
func newGetLogSettingLogic(ctx context.Context, deps Deps) *GetLogSettingLogic {
	return &GetLogSettingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *GetLogSettingLogic) GetLogSetting() (resp *dto.LogSetting, err error) {
	configs, err := l.deps.System.GetLogConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetLogSetting] Database query error", logger.Field("error", err.Error()))
		return nil, err
	}
	resp = &dto.LogSetting{}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
