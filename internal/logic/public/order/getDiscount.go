package order

import "github.com/perfect-panel/server/internal/model/dto"

func getDiscount(discounts []dto.SubscribeDiscount, inputMonths int64) float64 {
	var finalDiscount float64 = 100

	for _, discount := range discounts {
		if inputMonths >= discount.Quantity && discount.Discount < finalDiscount {
			finalDiscount = discount.Discount
		}
	}

	return finalDiscount / float64(100)
}
