package maintenance

import (
	"context"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/pkg/logger"
)

// RateHandler refreshes billing's cached exchange rate from the site
// currency to CNY. Without an exchange-rate API key there is nothing to
// refresh, and the cache keeps its current rate.
type RateHandler struct {
	deps RateDependencies
}

// NewRateHandler builds the refresh over the currency settings and billing's
// rate cache.
func NewRateHandler(deps RateDependencies) *RateHandler {
	return &RateHandler{deps: deps}
}

func (h *RateHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	log := logger.WithContext(ctx)
	currency, err := h.deps.System.GetCurrencyConfig(ctx)
	if err != nil {
		log.Errorw("[ExchangeRate] GetCurrencyConfig error", logger.Field("error", err.Error()))
		return err
	}
	configs := struct {
		CurrencyUnit   string
		CurrencySymbol string
		AccessKey      string
	}{}
	config.SystemConfigSliceReflectToStruct(currency, &configs)

	if configs.AccessKey == "" {
		log.Debugf("[ExchangeRate] skip exchange rate, no access key configured")
		return nil
	}
	result, err := billing.ConvertCurrency(configs.CurrencyUnit, "CNY", configs.AccessKey, 1)
	if err != nil {
		log.Errorw("[ExchangeRate] ConvertCurrency error", logger.Field("error", err.Error()))
		return err
	}
	h.deps.ExchangeRate.Set(result)
	log.Infof("[ExchangeRate] ConvertCurrency success, result: %+v", result)
	return nil
}
