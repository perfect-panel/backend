package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetRegisterConfig returns the stored registration settings.
func (s *Service) GetRegisterConfig(ctx context.Context) (*dto.RegisterConfig, error) {
	configs, err := s.deps.System.GetRegisterConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetRegisterConfig] query the register config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get register config error: %v", err.Error())
	}
	resp := &dto.RegisterConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
