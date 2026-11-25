// Package payment holds what every payment gateway protocol shares: the
// supported platforms and their names, and the money conversions between
// the minor units orders count and the decimal amounts gateways exchange.
package payment

import (
	"github.com/perfect-panel/backend/internal/infra/integration"
)

type Platform int

const (
	Stripe Platform = iota
	AlipayF2F
	EPay
	Balance
	Cryptomus
	GMPay
	PayPal
	Nano
	UNSUPPORTED Platform = -1
)

var platformNames = map[string]Platform{
	"Stripe":      Stripe,
	"AlipayF2F":   AlipayF2F,
	"EPay":        EPay,
	"balance":     Balance,
	"Cryptomus":   Cryptomus,
	"GMPay":       GMPay,
	"PayPal":      PayPal,
	"Nano":        Nano,
	"unsupported": UNSUPPORTED,
}

func (p Platform) String() string {
	for k, v := range platformNames {
		if v == p {
			return k
		}
	}
	return "unsupported"
}

func ParsePlatform(s string) Platform {
	if p, ok := platformNames[s]; ok {
		return p
	}
	return UNSUPPORTED
}

// SupportedPlatformNames lists every platform that may be exposed for a new
// checkout. Keep this separate from GetSupportedPlatforms because balance is
// an internal checkout method rather than an administrator-configurable
// gateway.
func SupportedPlatformNames() []string {
	return []string{Stripe.String(), AlipayF2F.String(), EPay.String(), Balance.String(), Cryptomus.String(), GMPay.String(), PayPal.String(), Nano.String()}
}

func GetSupportedPlatforms() []integration.Info {
	return []integration.Info{
		{
			Platform:    Stripe.String(),
			PlatformURL: "https://stripe.com",
			PlatformFieldDescription: map[string]string{
				"public_key":     "Publishable key",
				"secret_key":     "Secret key",
				"webhook_secret": "Webhook secret",
				"payment":        "Payment Method, only supported card/alipay/wechat_pay",
			},
		},
		{
			Platform:    AlipayF2F.String(),
			PlatformURL: "https://alipay.com",
			PlatformFieldDescription: map[string]string{
				"app_id":       "App ID",
				"private_key":  "Private Key",
				"public_key":   "Public Key",
				"invoice_name": "Invoice Name",
				"sandbox":      "Sandbox Mode",
			},
		},
		{
			Platform:    EPay.String(),
			PlatformURL: "",
			PlatformFieldDescription: map[string]string{
				"pid":           "Merchant ID",
				"url":           "Gateway URL",
				"key":           "V1 merchant key",
				"type":          "Payment type (empty for page cashier)",
				"version":       "Protocol: v1 (default) or v2",
				"checkout_mode": "Checkout: page (default) or api",
				"private_key":   "V2 merchant RSA private key",
				"public_key":    "V2 platform RSA public key",
			},
		},
		{
			Platform:    PayPal.String(),
			PlatformURL: "https://developer.paypal.com/",
			PlatformFieldDescription: map[string]string{
				"client_id":     "REST app client ID",
				"client_secret": "REST app secret",
				"webhook_id":    "Webhook ID from the same app and environment",
				"environment":   "live (default) or sandbox",
				"locale":        "Optional checkout language, e.g. zh-CN or en-US",
			},
		},
		{
			Platform:    GMPay.String(),
			PlatformURL: "https://github.com/GMWalletApp/epusdt",
			PlatformFieldDescription: map[string]string{
				"url":        "HTTPS base URL of the GMPay gateway",
				"pid":        "Merchant PID",
				"secret_key": "Merchant key",
			},
		},
		{
			Platform:    Cryptomus.String(),
			PlatformURL: "https://cryptomus.com",
			PlatformFieldDescription: map[string]string{
				"merchant_id": "Merchant UUID",
				"api_key":     "Payment API Key",
			},
		},
		{
			Platform:    Nano.String(),
			PlatformURL: "https://nano.org",
			PlatformFieldDescription: map[string]string{
				"pay_to":       "Nano account that receives payments (nano_...)",
				"node_url":     "Nano node RPC URL the payments are read from (https)",
				"currency":     "Currency the method charges (ISO code)",
				"xno_per_unit": "Price in XNO of one unit of that currency",
			},
		},
	}
}
