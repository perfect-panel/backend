package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateTosConfig stores the terms of service. No running subsystem reads
// them, so nothing is reloaded.
func (s *Service) UpdateTosConfig(ctx context.Context, req *dto.TosConfig) error {
	change := settingsChange{
		category: "tos",
		next:     convertedConfigFields(*req),
		previous: previousFields(ctx, "tos", s.GetTosConfig, convertedConfigFields),
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateTosConfig] update tos config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update tos config error: %v", err)
	}
	return nil
}
