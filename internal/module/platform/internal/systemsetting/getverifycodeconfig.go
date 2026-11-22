package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetVerifyCodeConfig returns the stored verification code settings.
func (s *Service) GetVerifyCodeConfig(ctx context.Context) (*dto.VerifyCodeConfig, error) {
	configs, err := s.deps.System.GetVerifyCodeConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetVerifyCodeConfig] query the verify code config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Get Verify Code Config Error: %s", err.Error())
	}
	resp := &dto.VerifyCodeConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
