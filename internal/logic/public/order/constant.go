package order

const (
	Epay            = "epay"
	AlipayF2f       = "alipay_f2f"
	StripeAlipay    = "stripe_alipay"
	StripeWeChatPay = "stripe_wechat_pay"
	Balance         = "balance"

	// MaxOrderAmount Order amount limits
	MaxOrderAmount    = 2147483647 // int32 max value (2.1 billion)
	MaxRechargeAmount = 2000000000 // 2 billion, slightly lower for safety
	MinRechargeAmount = 100        // minimum recharge amount in minor currency units
	MaxQuantity       = 1000       // Maximum quantity per order
)
