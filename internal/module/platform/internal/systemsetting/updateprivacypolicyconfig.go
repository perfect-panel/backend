package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdatePrivacyPolicyConfig stores the privacy policy in the tos settings
// category. No running subsystem reads it, so nothing is reloaded.
func (s *Service) UpdatePrivacyPolicyConfig(ctx context.Context, req *dto.PrivacyPolicyConfig) error {
	change := settingsChange{
		category: "tos",
		next:     convertedConfigFields(*req),
		previous: previousFields(ctx, "tos", s.GetPrivacyPolicyConfig, convertedConfigFields),
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdatePrivacyPolicyConfig] update tos config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update tos config error: %v", err)
	}
	return nil
}
