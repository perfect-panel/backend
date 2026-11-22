package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

func TestEPaySiteRelativeURLKeepsTheBrowserOnTheCurrentHost(t *testing.T) {
	var queried string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queried = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"succ","trade_no":"T1","out_trade_no":"O1","type":"alipay","pid":"1","money":"1.00","status":0}`))
	}))
	defer server.Close()

	registry := NewRegistry(WithHTTPClient(server.Client()))
	gw, err := registry.Open(&payment.Payment{Platform: "EPay", Domain: server.URL + "/", Config: `{"pid":"1","url":"/pay","key":"k","type":"alipay"}`})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := gw.StartPayment(context.Background(), Checkout{
		Order:     &order.Order{OrderNo: "O1"},
		Charge:    Charge{Amount: 100, Currency: "CNY"},
		NotifyURL: "https://api.example/v1/notify/EPay/tok",
		ReturnURL: "https://mirror.example/order",
		Subject:   "plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.CheckoutUrl, "/pay/submit.php?") {
		t.Fatalf("checkout URL %q is not site-relative", resp.CheckoutUrl)
	}
	params, err := url.ParseQuery(strings.TrimPrefix(resp.CheckoutUrl, "/pay/submit.php?"))
	if err != nil || params.Get("out_trade_no") != "O1" || params.Get("sign") == "" {
		t.Fatalf("checkout parameters %v (%v)", params, err)
	}

	result, err := gw.(*epayGateway).client.QueryOrder(context.Background(), "O1")
	if err != nil {
		t.Fatal(err)
	}
	if queried != "/pay/api.php" || !result.Unpaid {
		t.Fatalf("server-side query went to %q, unpaid=%v", queried, result.Unpaid)
	}
}

func TestEPayRelativeConfigurationSurvivesNormalization(t *testing.T) {
	for _, endpoint := range []string{"/pay", "//pay.example"} {
		normalized, err := NewRegistry().NormalizeConfig("EPay", map[string]any{
			"pid": "1", "url": endpoint, "key": "k", "type": "alipay",
		})
		if err != nil {
			t.Fatal(err)
		}
		var config payment.EPayConfig
		if err := json.Unmarshal([]byte(normalized), &config); err != nil || config.Url != endpoint {
			t.Fatalf("normalized relative address changed: %v", err)
		}
	}
	for _, endpoint := range []string{"/pay?key=x", "/pay#fragment", "///host"} {
		if _, err := NewRegistry().NormalizeConfig("EPay", map[string]any{"pid": "1", "url": endpoint, "key": "k"}); err == nil {
			t.Fatalf("invalid relative address accepted: %q", endpoint)
		}
	}
}

func TestEPaySchemeRelativeCheckoutUsesHTTPSForServerRequests(t *testing.T) {
	var queried bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queried = true
		_, _ = w.Write([]byte(`{"code":1,"pid":"1","out_trade_no":"O1","status":0}`))
	}))
	defer server.Close()
	endpoint := strings.TrimPrefix(server.URL, "https:")
	gw, err := NewRegistry(WithHTTPClient(server.Client())).Open(&payment.Payment{
		Platform: "EPay", Config: `{"pid":"1","url":"` + endpoint + `","key":"k","type":"alipay"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := gw.StartPayment(context.Background(), Checkout{Order: &order.Order{OrderNo: "O1"}, Charge: Charge{Amount: 100}})
	if err != nil || !strings.HasPrefix(resp.CheckoutUrl, endpoint+"/submit.php?") {
		t.Fatalf("scheme-relative checkout failed: %v", err)
	}
	if _, err := gw.(*epayGateway).client.QueryOrder(context.Background(), "O1"); err != nil || !queried {
		t.Fatalf("HTTPS query failed: %v", err)
	}
}

func TestEPaySiteRelativeURLRequiresTheMethodDomain(t *testing.T) {
	if _, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Config: `{"pid":"1","url":"/pay","key":"k","type":"alipay"}`}); err == nil {
		t.Fatal("a site-relative URL opened without a domain to reach it from the server")
	}
}

func TestEPayAbsoluteURLIsUnchanged(t *testing.T) {
	gw, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Domain: "https://api.example", Config: `{"pid":"1","url":"https://pay.example","key":"k","type":"alipay"}`})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := gw.StartPayment(context.Background(), Checkout{Order: &order.Order{OrderNo: "O1"}, Charge: Charge{Amount: 100, Currency: "CNY"}, NotifyURL: "https://api.example/n", Subject: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.CheckoutUrl, "https://pay.example/submit.php?") {
		t.Fatalf("absolute checkout URL changed: %q", resp.CheckoutUrl)
	}
	if _, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Domain: "https://api.example", Config: `{"pid":"1","url":"//pay.example","key":"k","type":"alipay"}`}); err != nil {
		t.Fatalf("a scheme-relative URL is not treated as site-relative: %v", err)
	}
}
