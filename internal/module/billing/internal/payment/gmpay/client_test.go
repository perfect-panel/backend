package gmpay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Published GMPay merchant API examples, with literal expected signatures
// independent of this implementation.
func TestGMPayMerchantSignatureVectors(t *testing.T) {
	const secret = "epusdt_secret_key"
	params := map[string]string{"pid": "1000", "order_id": "ORD202605230001", "currency": "cny", "token": "usdt", "network": "tron", "amount": "100", "notify_url": "https://merchant.example/notify", "redirect_url": "https://merchant.example/return", "name": "VIP"}
	if got := signature(params, secret); got != "6f874b1919d95081835e2809b620e354a5866f5a6dbb2e432d1627f1eb10059d" {
		t.Fatalf("create signature = %q", got)
	}
	const body = `{"pid":"1000","trade_id":"20260523171652123456001","order_id":"ORD202605230001","amount":100.0000,"actual_amount":14.29,"receive_address":"TTestTronAddress001","token":"USDT","block_transaction_id":"0xabc123...","signature":"498975a97bc34563bdb14df53fc18054645df9684d6c67d9b9dd90ec62be1018","status":2}`
	c, err := NewClient(Config{URL: "https://gateway.example", PID: "1000", Secret: secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.VerifyNotification([]byte(body))
	if err != nil || n.Amount != 10000 || n.OrderID != params["order_id"] {
		t.Fatalf("notification = %+v, %v", n, err)
	}
	for _, bad := range []string{
		strings.Replace(body, "14.29", "14.28", 1),
		strings.Replace(body, "100.0000", "100.000000000000000001", 1),
		strings.Replace(body, `"status":2`, `"status":2,"status":2`, 1),
		strings.Replace(body, `"status":2`, `"status":5`, 1),
		strings.Replace(body, `"pid":"1000"`, `"pid":"2000"`, 1),
		body + `{}`,
	} {
		if _, err := c.VerifyNotification([]byte(bad)); err == nil {
			t.Error("accepted malformed or altered notification")
		}
	}
	c.config.PID = "2000"
	if _, err := c.VerifyNotification([]byte(body)); err == nil {
		t.Error("accepted signed callback for another merchant")
	}
}

func TestGMPayCreateForCashierSelection(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f := r.PostForm
		if f.Has("token") || f.Has("network") || f.Has("payment_type") {
			t.Errorf("unexpected asset selection: %v", f)
		}
		if f.Get("signature") != "dad038fddc681f06019fffbca62e6a6cd2a9ef2232ffa1aed18af232850e0ee2" {
			t.Errorf("wrong signature for transmitted form: %s", f.Get("signature"))
		}
		_, _ = fmt.Fprint(w, `{"status_code":200,"data":{"order_id":"order-1","trade_id":"test_trade-1","amount":12.3400,"currency":"USD","payment_url":"https://gateway.example/cashier/test_trade-1","status":4}}`)
	}))
	defer s.Close()
	c, err := NewClient(Config{URL: s.URL, PID: "1001", Secret: "sample"}, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	r := CreateRequest{OrderID: "order-1", Amount: 1234, Currency: "USD", NotifyURL: "https://panel.example/notify"}
	if _, err := c.Create(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestGMPayMerchantConfiguration(t *testing.T) {
	valid := Config{URL: "https://gateway.example/gm/", PID: "1000", Secret: "test"}
	if err := valid.Validate(); err != nil || valid.URL != "https://gateway.example/gm" {
		t.Fatalf("gateway base: %s, %v", valid.URL, err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.URL = "http://gateway.example" },
		func(c *Config) { c.URL = "https://gateway.example/a/../b" },
		func(c *Config) { c.URL = "https://gateway.example/?x=1" },
		func(c *Config) { c.URL = "https://user:key@gateway.example" },
		func(c *Config) { c.PID = "" },
		func(c *Config) { c.Secret = "" },
	} {
		bad := valid
		change(&bad)
		if err := bad.Validate(); err == nil {
			t.Error("accepted incomplete or invalid configuration")
		}
	}
}
