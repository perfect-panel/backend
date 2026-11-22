package auditlog

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
)

// GetLogSetting returns the stored log retention settings.
func (s *Service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	configs, err := s.deps.System.GetLogConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetLogSetting] Database query error", logger.Field("error", err.Error()))
		return nil, err
	}
	resp := &dto.LogSetting{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
