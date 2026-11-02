package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	"github.com/perfect-panel/server/internal/module/billing/entity/payment"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

type epayV1APIPeer struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newEPayV1APIPeer(t *testing.T, created string) *epayV1APIPeer {
	t.Helper()
	peer := &epayV1APIPeer{}
	peer.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer.mu.Lock()
		peer.paths = append(peer.paths, r.URL.Path)
		peer.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/mapi.php") {
			_, _ = fmt.Fprint(w, created)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":1,"msg":"succ","trade_no":"T1","out_trade_no":"O1","type":"alipay","pid":"1","money":"1.00","status":0}`)
	}))
	t.Cleanup(peer.Close)
	return peer
}

func (p *epayV1APIPeer) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

func epayV1APIConfig(endpoint string) string {
	return `{"pid":"1","url":"` + endpoint + `","key":"k","type":"alipay","checkout_mode":"api"}`
}

func epayV1APICheckout() (context.Context, Checkout) {
	ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.7", "test-agent"))
	return ctx, Checkout{
		Order:     &order.Order{OrderNo: "O1"},
		Charge:    Charge{Amount: 100, Currency: "CNY"},
		Subject:   "plan",
		NotifyURL: "https://api.example/v1/notify/EPay/tok",
		ReturnURL: "https://mirror.example/order",
		BeginSession: func(context.Context) (bool, error) {
			return true, nil
		},
		CompleteSession: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}
}

func TestEPayV1APISiteRelativeUsesTheMethodDomain(t *testing.T) {
	for _, tc := range []struct{ created, kind, entry string }{
		{`{"code":1,"trade_no":"T1","qrcode":"https://qr.example/abc"}`, "qr", "https://qr.example/abc"},
		{`{"code":1,"trade_no":"T1","payurl":"https://pay.example/checkout"}`, "url", "https://pay.example/checkout"},
	} {
		peer := newEPayV1APIPeer(t, tc.created)
		gw, err := NewRegistry(WithHTTPClient(peer.Client())).Open(&payment.Payment{
			Platform: "EPay", Domain: peer.URL + "/", Config: epayV1APIConfig("/pay"),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, checkout := epayV1APICheckout()
		resp, err := gw.StartPayment(ctx, checkout)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Type != tc.kind || resp.CheckoutUrl != tc.entry {
			t.Fatalf("checkout %+v, want %s %s", resp, tc.kind, tc.entry)
		}
		if _, err := gw.(*epayGateway).client.QueryOrder(context.Background(), "O1"); err != nil {
			t.Fatal(err)
		}
		if got := peer.requests(); len(got) != 2 || got[0] != "/pay/mapi.php" || got[1] != "/pay/api.php" {
			t.Fatalf("server-side requests went to %v", got)
		}
	}
}

func TestEPayV1APIDeploymentPathAndSchemeRelativeURL(t *testing.T) {
	peer := newEPayV1APIPeer(t, `{"code":1,"trade_no":"T1","qrcode":"https://qr.example/abc"}`)
	registry := NewRegistry(WithHTTPClient(peer.Client()))
	for _, endpoint := range []string{peer.URL + "/pay/", strings.TrimPrefix(peer.URL, "https:") + "/pay"} {
		gw, err := registry.Open(&payment.Payment{Platform: "EPay", Config: epayV1APIConfig(endpoint)})
		if err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		ctx, checkout := epayV1APICheckout()
		if _, err := gw.StartPayment(ctx, checkout); err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
	}
	if got := peer.requests(); len(got) != 2 || got[0] != "/pay/mapi.php" || got[1] != "/pay/mapi.php" {
		t.Fatalf("server-side requests went to %v", got)
	}
}

func TestEPayV1APISiteRelativeURLRequiresAnHTTPSDomain(t *testing.T) {
	for _, domain := range []string{"", "http://api.example"} {
		if _, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Domain: domain, Config: epayV1APIConfig("/pay")}); err == nil {
			t.Fatalf("site-relative V1 API URL opened with domain %q", domain)
		}
	}
}

func TestEPayV1APIRelativeConfigurationSurvivesNormalization(t *testing.T) {
	registry := NewRegistry()
	normalize := func(endpoint string) (string, error) {
		var config map[string]any
		if err := json.Unmarshal([]byte(epayV1APIConfig(endpoint)), &config); err != nil {
			t.Fatal(err)
		}
		return registry.NormalizeConfig("EPay", config)
	}
	for _, endpoint := range []string{"/pay", "/pay/", "//pay.example/pay", "https://pay.example/pay", "https://pay.example"} {
		normalized, err := normalize(endpoint)
		if err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		var config payment.EPayConfig
		if err := json.Unmarshal([]byte(normalized), &config); err != nil || config.Url != endpoint {
			t.Fatalf("%s: normalized address changed to %q (%v)", endpoint, config.Url, err)
		}
	}
	for _, endpoint := range []string{
		"/pay?key=x", "/pay#fragment", "///host", "/pay/../admin", "/pay/%2e%2e", "http://pay.example/pay",
		"https://pay.example/submit.php", "https://pay.example/pay/api.php", "/pay/mapi.php",
	} {
		if _, err := normalize(endpoint); err == nil {
			t.Fatalf("invalid V1 API address accepted: %q", endpoint)
		}
	}
}
