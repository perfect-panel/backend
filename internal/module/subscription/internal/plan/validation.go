package plan

import (
	"fmt"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func validateSubscribeInput(unitTime string, unitPrice, replacement, inventory, traffic, speedLimit, deviceLimit, quota, deductionRatio, resetCycle int64, discounts []dto.SubscribeDiscount) error {
	_, unitErr := period.ParseUnit(unitTime)
	if unitErr != nil || !period.Cycle(resetCycle).Valid() || unitPrice < 0 || replacement < 0 || inventory < -1 || traffic < 0 || speedLimit < 0 || deviceLimit < 0 || quota < 0 || deductionRatio < 0 || deductionRatio > 100 {
		return fmt.Errorf("invalid subscription configuration: %w", xerr.NewErrCodeMsg(400, "INVALID_SUBSCRIBE_CONFIGURATION"))
	}
	for _, discount := range discounts {
		if discount.Quantity <= 0 || discount.Discount <= 0 || discount.Discount > 100 {
			return fmt.Errorf("invalid subscription discount: %s: %w", fmt.Sprint(discount), xerr.NewErrCodeMsg(400, "INVALID_SUBSCRIBE_DISCOUNT"))
		}
	}
	return nil
}
