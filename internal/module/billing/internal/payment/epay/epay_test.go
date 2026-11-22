package epay

import (
	"net/url"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
)

// The pay URL must carry exactly the amount the order expects: every amount
// is sent as its exact two-decimal form, never through a float.
func TestCreatePayUrlSendsExactAmounts(t *testing.T) {
	client := NewClient("1001", "https://pay.example", "secret", "alipay")
	for amount := int64(1); amount <= 100000; amount++ {
		payURL, err := client.CreatePayUrl(Order{Name: "product", OrderNo: "order-1", Amount: amount})
		if err != nil {
			t.Fatalf("CreatePayUrl(%d): %v", amount, err)
		}
		parsed, err := url.Parse(payURL)
		if err != nil {
			t.Fatal(err)
		}
		sent, err := payment.ParseAmount(parsed.Query().Get("money"))
		if err != nil || sent != amount {
			t.Fatalf("amount %d was sent as %q", amount, parsed.Query().Get("money"))
		}
	}
}

func TestCreatePayUrlRejectsUnsupportedEndpoint(t *testing.T) {
	for _, endpoint := range []string{"ftp://pay.example", "https://pay.example/?x=1", "not a url", "https://"} {
		if _, err := NewClient("1001", endpoint, "secret", "alipay").CreatePayUrl(Order{OrderNo: "order-1", Amount: 100}); err == nil {
			t.Errorf("endpoint %q accepted", endpoint)
		}
	}
}

// A signature produced by a real gateway for a known merchant key verifies.
func TestVerifySignAcceptsGatewayVector(t *testing.T) {
	params := map[string]string{
		"pid":          "1654",
		"trade_no":     "2024121521150860990",
		"out_trade_no": "202412152115078262977262254",
		"type":         "alipay",
		"name":         "product",
		"money":        "10",
		"trade_status": "TRADE_SUCCESS",
		"sign":         "d3181f18ebdf9821f0ab6ee93faa82d1",
		"sign_type":    "MD5",
	}
	client := NewClient("1654", "https://pay.example", "LbTabbB580zWyhXhyyww7wwvy5u8k0wl", "alipay")
	if !client.VerifySign(params) {
		t.Fatal("gateway signature rejected")
	}
	params["money"] = "11"
	if client.VerifySign(params) {
		t.Fatal("changed amount still verified")
	}
}
