package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetCurrencyConfig returns the stored currency settings, the exchange-rate
// provider's access key masked.
func (s *Service) GetCurrencyConfig(ctx context.Context) (*dto.CurrencyConfig, error) {
	resp, err := s.storedCurrencyConfig(ctx)
	if err != nil {
		return nil, err
	}
	maskCurrencySecrets(resp)
	return resp, nil
}

// storedCurrencyConfig reads the currency settings as stored, the access key
// in clear.
func (s *Service) storedCurrencyConfig(ctx context.Context) (*dto.CurrencyConfig, error) {
	configs, err := s.deps.System.GetCurrencyConfig(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetCurrencyConfig] query the currency config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetCurrencyConfig error: %v", err.Error())
	}
	resp := &dto.CurrencyConfig{}
	config.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
