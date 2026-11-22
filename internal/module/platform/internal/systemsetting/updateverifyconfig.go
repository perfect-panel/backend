package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateVerifyConfig stores the verification settings and reloads the verify
// subsystem from them. A masked Turnstile secret keeps the stored one.
func (s *Service) UpdateVerifyConfig(ctx context.Context, req *dto.VerifyConfig) error {
	stored, err := s.storedVerifyConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateVerifyConfig] stored verify config could not be read", logger.Field("error", err.Error()))
	}
	if err := keepVerifySecrets(req, stored); err != nil {
		return err
	}
	change := settingsChange{category: "verify", next: convertedConfigFields(*req)}
	if stored != nil {
		change.previous = convertedConfigFields(*stored)
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateVerifyConfig] update verify config error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update verify config error: %v", err)
	}
	return s.deps.reinit("verify")
}
