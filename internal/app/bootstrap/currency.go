package bootstrap

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger"
)

// currencySettings mirrors the stored currency keys. The seeded Currency key
// has no reader: the unit is what the runtime uses.
type currencySettings struct {
	CurrencyUnit   string
	CurrencySymbol string
	AccessKey      string
}

// Currency loads the site currency and resets billing's cached exchange rate
// to zero; the exchange-rate task refreshes it when an API key is configured.
func Currency(ctx context.Context, deps *Dependencies) error {
	var configs currencySettings
	if err := readSettings(ctx, categoryCurrency, deps.Settings.GetCurrencyConfig, &configs); err != nil {
		logger.WithContext(ctx).Errorf("[INIT] Failed to get currency configuration: %v", err.Error())
		return err
	}
	deps.ExchangeRate.Set(0)
	currencyConfig := config.Currency{
		Unit:      configs.CurrencyUnit,
		Symbol:    configs.CurrencySymbol,
		AccessKey: configs.AccessKey,
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Currency = currencyConfig })
	logger.WithContext(ctx).Info("[INIT] Currency configuration loaded",
		logger.Field("unit", currencyConfig.Unit),
		logger.Field("symbol", currencyConfig.Symbol),
		logger.Field("provider_configured", currencyConfig.AccessKey != ""),
	)
	return nil
}
