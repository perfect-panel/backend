package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSiteConfig returns the stored site settings.
func (s *Service) GetSiteConfig(ctx context.Context) (*dto.SiteConfig, error) {
	configs, err := s.deps.System.GetSiteConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetSiteConfig] query the site config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get site config failed: %v", err.Error())
	}
	resp := &dto.SiteConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
