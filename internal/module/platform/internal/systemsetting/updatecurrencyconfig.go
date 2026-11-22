package systemsetting

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateCurrencyConfig stores the currency settings and, once they are
// stored, reloads the currency subsystem: a failed write must not reload a
// configuration that did not change. A masked access key keeps the stored
// one.
func (s *Service) UpdateCurrencyConfig(ctx context.Context, req *dto.CurrencyConfig) error {
	stored, err := s.storedCurrencyConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateCurrencyConfig] stored currency config could not be read", logger.Field("error", err.Error()))
	}
	if err := keepCurrencySecrets(req, stored); err != nil {
		return err
	}
	change := settingsChange{category: "currency", next: convertedConfigFields(*req)}
	if stored != nil {
		change.previous = convertedConfigFields(*stored)
	}
	if err := updateConfigFields(ctx, s.deps, change); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update currency config: %v", err)
	}
	return s.deps.reinit("currency")
}
