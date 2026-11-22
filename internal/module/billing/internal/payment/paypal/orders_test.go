package paypal

import (
	"encoding/json"
	"testing"
)

func TestPayPalCurrencyPrecision(t *testing.T) {
	var client Client
	for _, tc := range []struct {
		currency string
		amount   int64
		want     string
	}{
		{"USD", 123, "1.23"}, {"JPY", 200, "2"}, {"HUF", 100, "1"}, {"TWD", 9900, "99"}, {"JPY", 250, ""}, {"USD", 0, ""},
	} {
		raw, err := client.CreateBody("order", tc.amount, tc.currency, "https://panel.example/result")
		if tc.want == "" {
			if err == nil {
				t.Fatal("accepted unrepresentable amount")
			}
			continue
		}
		var data Order
		if err != nil || json.Unmarshal(raw, &data) != nil || len(data.Units) != 1 || data.Units[0].Amount.Value != tc.want {
			t.Fatalf("%s: %s, %v", tc.currency, raw, err)
		}
	}
}

func TestPayPalCaptureProof(t *testing.T) {
	base := Order{ID: "ORDER1", Intent: "CAPTURE", Status: "COMPLETED", Units: []PurchaseUnit{{Reference: "one", Invoice: "one", Amount: Money{"USD", "12.34"}}}}
	u := &base.Units[0]
	c := Capture{"CAP1", "COMPLETED", u.Amount}
	u.Payments.Captures = []Capture{c}
	if !base.Paid(1234, "USD", "CAP1") {
		t.Fatal("matching capture rejected")
	}
	if base.Paid(1234, "USD", "wrong") || base.Paid(1200, "USD", "CAP1") || base.Paid(1234, "EUR", "CAP1") {
		t.Fatal("accepted mismatched capture")
	}
	u.Payments.Refunds = []json.RawMessage{json.RawMessage(`{}`)}
	if base.Paid(1234, "USD", "CAP1") {
		t.Fatal("refunded order accepted")
	}
	u.Payments.Refunds = nil
	u.Payments.Captures = append(u.Payments.Captures, c)
	if base.Paid(1234, "USD", "CAP1") {
		t.Fatal("unexpected additional capture accepted")
	}
}

func TestPayPalApprovalLinkTrust(t *testing.T) {
	for _, raw := range []string{"https://evil.example/checkout", "https://www.paypal.com.evil.example", "http://www.paypal.com", "https://user@www.paypal.com"} {
		if ApprovalURL(raw) {
			t.Fatalf("accepted unexpected redirect %q", raw)
		}
	}
}

func TestPayPalCheckoutLocale(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"  ", ""}, {" zh-cn ", "zh-CN"}, {"en-US", "en-US"}, {"ZH-tw", "zh-TW"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			cfg := Config{ClientID: "client", ClientSecret: "secret", WebhookID: "hook", Locale: tc.input}
			client, err := NewClient(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := client.CreateBody("order", 100, "USD", "https://panel.example/return")
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				Source struct {
					PayPal struct {
						Experience map[string]string `json:"experience_context"`
					} `json:"paypal"`
				} `json:"payment_source"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			got, sent := body.Source.PayPal.Experience["locale"]
			if got != tc.want || sent != (tc.want != "") {
				t.Errorf("locale = %q, present=%v", got, sent)
			}
		})
	}
	for _, value := range []string{"zh", "en_US", "zh-Hans-CN", "12-US"} {
		cfg := Config{ClientID: "client", ClientSecret: "secret", Locale: value}
		if err := cfg.Validate(); err == nil {
			t.Errorf("accepted invalid checkout locale %q", value)
		}
	}
}
