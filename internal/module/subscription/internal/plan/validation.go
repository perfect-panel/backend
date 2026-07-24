package plan

import (
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func validateSubscribeInput(unitTime string, unitPrice, replacement, inventory, traffic, speedLimit, deviceLimit, quota, deductionRatio, resetCycle int64, discounts []dto.SubscribeDiscount) error {
	validUnit := map[string]bool{"Year": true, "Month": true, "Day": true, "Hour": true, "Minute": true, "NoLimit": true}
	if !validUnit[unitTime] || unitPrice < 0 || replacement < 0 || inventory < -1 || traffic < 0 || speedLimit < 0 || deviceLimit < 0 || quota < 0 || deductionRatio < 0 || deductionRatio > 100 || resetCycle < 0 || resetCycle > 3 {
		return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_SUBSCRIBE_CONFIGURATION"), "invalid subscription configuration")
	}
	for _, discount := range discounts {
		if discount.Quantity <= 0 || discount.Discount <= 0 || discount.Discount > 100 {
			return errors.Wrapf(xerr.NewErrCodeMsg(400, "INVALID_SUBSCRIBE_DISCOUNT"), "invalid subscription discount: %s", fmt.Sprint(discount))
		}
	}
	return nil
}
