package order

import "github.com/perfect-panel/server/internal/model/entity/payment"

func calculateFee(amount int64, config *payment.Payment) int64 {
	var fee float64
	switch config.FeeMode {
	case 0:
		return 0
	case 1:
		fee = float64(amount) * (float64(config.FeePercent) / float64(100))
	case 2:
		if amount > 0 {
			fee = float64(config.FeeAmount)
		}
	case 3:
		fee = float64(amount)*(float64(config.FeePercent)/float64(100)) + float64(config.FeeAmount)
	}
	return int64(fee)
}
