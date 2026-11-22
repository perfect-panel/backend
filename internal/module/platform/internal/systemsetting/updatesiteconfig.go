package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateSiteConfig stores the site settings, every value as written, and
// reloads the site subsystem.
func (s *Service) UpdateSiteConfig(ctx context.Context, req *dto.SiteConfig) error {
	change := settingsChange{
		category: "site",
		next:     stringConfigFields(*req),
		previous: previousFields(ctx, "site", s.GetSiteConfig, stringConfigFields),
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateSiteConfig] update site config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update site config error: %v", err.Error())
	}
	return s.deps.reinit("site")
}
