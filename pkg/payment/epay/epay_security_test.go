package epay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVerifySignRejectsMissingAndInvalidSignatures(t *testing.T) {
	client := NewClient("merchant-1", "https://pay.example", "secret", "alipay")
	params := map[string]string{
		"pid":          "merchant-1",
		"out_trade_no": "order-1",
		"trade_no":     "trade-1",
		"money":        "10.00",
		"trade_status": "TRADE_SUCCESS",
		"type":         "alipay",
		"sign_type":    "MD5",
	}

	if client.VerifySign(params) {
		t.Fatal("missing signature must be rejected")
	}
	params["sign"] = "00000000000000000000000000000000"
	if client.VerifySign(params) {
		t.Fatal("invalid signature must be rejected")
	}
	params["sign"] = client.createSign(params)
	if !client.VerifySign(params) {
		t.Fatal("valid signature must be accepted")
	}
	params["money"] = "0.01"
	if client.VerifySign(params) {
		t.Fatal("changing a signed parameter must invalidate the signature")
	}
}

func TestParseMoneyUsesExactMinorUnits(t *testing.T) {
	tests := []struct {
		value   string
		want    int64
		wantErr bool
	}{
		{value: "0", want: 0},
		{value: "10", want: 1000},
		{value: "10.1", want: 1010},
		{value: "10.01", want: 1001},
		{value: "1.001", wantErr: true},
		{value: "-1.00", wantErr: true},
		{value: "1e2", wantErr: true},
		{value: " 1.00", wantErr: true},
		{value: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseMoney(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseMoney(%q) error=%v", test.value, err)
			}
			if err == nil && got != test.want {
				t.Fatalf("ParseMoney(%q)=%d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestCreatePaymentSubmitKeepsLegacyRedirect(t *testing.T) {
	client := NewClient("1001", "https://pay.example/", "secret", "alipay", ModeSubmit)
	result, err := client.CreatePayment(context.Background(), Order{
		Name: "product", OrderNo: "order-1", Amount: 10,
		NotifyUrl: "https://merchant.example/notify", ReturnUrl: "https://merchant.example/return",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if result.Type != "url" || !strings.HasPrefix(result.URL, "https://pay.example/submit.php?") {
		t.Fatalf("unexpected submit result: %+v", result)
	}
	parsed, err := url.Parse(result.URL)
	if err != nil {
		t.Fatal(err)
	}
	params := make(map[string]string, len(parsed.Query()))
	for name := range parsed.Query() {
		params[name] = parsed.Query().Get(name)
	}
	if params["sign_type"] != "MD5" || !client.VerifySign(params) {
		t.Fatalf("submit redirect has invalid signature: %+v", params)
	}
}

func TestCreatePaymentMAPIUsesFormPOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s, want POST", r.Method)
		}
		if r.URL.Path != "/gateway/mapi.php" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Errorf("content-type=%q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		params := make(map[string]string, len(r.PostForm))
		for name := range r.PostForm {
			params[name] = r.PostForm.Get(name)
		}
		client := NewClient("1001", "https://gateway.example", "secret", "alipay", ModeMAPI)
		if params["clientip"] != "203.0.113.9" || !client.VerifySign(params) {
			t.Errorf("unexpected mapi params: %+v", params)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 1, "trade_no": "trade-1", "qrcode": "weixin://pay/example",
		})
	}))
	defer server.Close()

	client := NewClient("1001", server.URL+"/gateway", "secret", "alipay", ModeMAPI)
	result, err := client.CreatePayment(context.Background(), Order{
		Name: "product", OrderNo: "order-1", Amount: 10,
		NotifyUrl: "https://merchant.example/notify", ClientIP: "203.0.113.9",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if result.Type != "qr" || result.URL != "weixin://pay/example" || result.TradeNo != "trade-1" {
		t.Fatalf("unexpected mapi result: %+v", result)
	}
}

func TestQueryOrderReturnsAuthoritativeGatewayFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/gateway/api.php" {
			t.Errorf("path=%q", got)
		}
		for name, want := range map[string]string{
			"act": "order", "pid": "1001", "key": "secret", "out_trade_no": "order-1",
		} {
			if got := r.URL.Query().Get(name); got != want {
				t.Errorf("query %s=%q, want %q", name, got, want)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 1, "msg": "ok", "pid": 1001, "trade_no": "trade-1",
			"out_trade_no": "order-1", "type": "alipay", "money": "10.00", "status": 1,
		})
	}))
	defer server.Close()

	client := NewClient("1001", server.URL+"/gateway", "secret", "alipay")
	result, err := client.QueryOrder("order-1")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}
	if result.MerchantID != "1001" || result.OrderNo != "order-1" || result.TradeNo != "trade-1" || result.Money != "10.00" || !result.Paid {
		t.Fatalf("unexpected query result: %+v", result)
	}
}

func TestQueryOrderRejectsUnsuccessfulLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"not found"}`))
	}))
	defer server.Close()

	client := NewClient("1001", server.URL, "secret", "alipay")
	if _, err := client.QueryOrder("order-1"); err == nil {
		t.Fatal("unsuccessful gateway lookup must be rejected")
	}
}

func TestQueryOrderReportsUnsupportedWhenGatewayReturnsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient("1001", server.URL, "secret", "alipay")
	if _, err := client.QueryOrder("order-1"); !errors.Is(err, ErrQueryNotSupported) {
		t.Fatalf("QueryOrder error=%v, want ErrQueryNotSupported", err)
	}
}

func TestQueryOrderDoesNotTreatOtherHTTPFailuresAsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gateway failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient("1001", server.URL, "secret", "alipay")
	if _, err := client.QueryOrder("order-1"); errors.Is(err, ErrQueryNotSupported) {
		t.Fatal("only a 404 response may be treated as an unsupported query API")
	}
}
