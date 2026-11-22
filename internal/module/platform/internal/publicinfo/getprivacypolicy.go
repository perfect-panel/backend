package publicinfo

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetPrivacyPolicy returns the privacy policy, which is stored with the
// terms of service.
func (s *Service) GetPrivacyPolicy(ctx context.Context) (*dto.PrivacyPolicyConfig, error) {
	configs, err := s.deps.Settings.GetTosConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetPrivacyPolicy] GetTosConfig error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetTosConfig error: %v", err.Error())
	}
	resp := &dto.PrivacyPolicyConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
