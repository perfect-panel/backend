package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscribeConfig returns the stored subscription settings.
func (s *Service) GetSubscribeConfig(ctx context.Context) (*dto.SubscribeConfig, error) {
	configs, err := s.deps.System.GetSubscribeConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetSubscribeConfig] query the subscribe config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscribe config failed: %v", err.Error())
	}
	resp := &dto.SubscribeConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
