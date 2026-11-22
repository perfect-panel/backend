// Package maintenance holds the queue handlers of the maintenance tasks: the
// quota grants administrators schedule and the daily exchange-rate refresh.
package maintenance

import (
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/repository"
)

// RateDependencies are the exchange-rate refresh's: the currency settings
// in the system settings (the platform kernel's) and the rate cache.
type RateDependencies struct {
	System       repository.SystemRepo
	ExchangeRate *billing.CurrencyRateCache
}
