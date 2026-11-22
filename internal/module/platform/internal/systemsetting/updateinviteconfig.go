package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateInviteConfig stores the invitation settings and reloads the invite
// subsystem.
func (s *Service) UpdateInviteConfig(ctx context.Context, req *dto.InviteConfig) error {
	change := settingsChange{
		category: "invite",
		next:     convertedConfigFields(*req),
		previous: previousFields(ctx, "invite", s.GetInviteConfig, convertedConfigFields),
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateInviteConfig] update invite config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update invite config error: %v", err)
	}
	return s.deps.reinit("invite")
}
