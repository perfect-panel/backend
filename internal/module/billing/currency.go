package billing

import "github.com/perfect-panel/backend/internal/module/billing/internal/exchangerate"

// CurrencyRateCache is shared with application assembly and the refresh worker.
// Provider communication remains private to billing.
type CurrencyRateCache = exchangerate.Cache

func NewCurrencyRateCache(rate float64) *CurrencyRateCache {
	return exchangerate.NewCache(rate)
}

func ConvertCurrency(from, to, accessKey string, amount float64) (float64, error) {
	return exchangerate.GetExchangeRate(from, to, accessKey, amount)
}
