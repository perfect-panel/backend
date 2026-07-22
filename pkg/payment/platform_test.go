package payment

import "testing"

func TestCryptoSaaSIsNotASupportedPaymentPlatform(t *testing.T) {
	if ParsePlatform("CryptoSaaS") != UNSUPPORTED {
		t.Fatal("CryptoSaaS must not be recognized as a payment platform")
	}
	for _, name := range SupportedPlatformNames() {
		if name == "CryptoSaaS" {
			t.Fatal("CryptoSaaS must not be exposed to checkout")
		}
	}
}
