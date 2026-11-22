package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetPrivacyPolicyConfig returns the stored privacy policy, which lives in
// the tos settings category.
func (s *Service) GetPrivacyPolicyConfig(ctx context.Context) (*dto.PrivacyPolicyConfig, error) {
	configs, err := s.deps.System.GetTosConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetPrivacyPolicyConfig] query the tos config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetTosConfig error: %v", err.Error())
	}
	resp := &dto.PrivacyPolicyConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
