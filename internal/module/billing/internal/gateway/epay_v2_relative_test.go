package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

type epayV2Paths struct {
	mu    sync.Mutex
	paths []string
}

func (p *epayV2Paths) add(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = append(p.paths, path)
}

func (p *epayV2Paths) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

func newRelativeEPayV2(t *testing.T) (*billingtest.EPayV2, *epayV2Paths) {
	t.Helper()
	paths := &epayV2Paths{}
	var peer *billingtest.EPayV2
	peer = billingtest.NewEPayV2(t, func(w http.ResponseWriter, r *http.Request) {
		paths.add(r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/pay/create"):
			peer.Reply(w, map[string]any{"code": 0, "trade_no": "trade-1", "pay_type": "jump", "pay_info": "https://pay.example/checkout"})
		case strings.HasSuffix(r.URL.Path, "/api/pay/query"):
			peer.Reply(w, epayV2Query(1))
		default:
			http.NotFound(w, r)
		}
	})
	return peer, paths
}

func relativeEPayV2Config(t *testing.T, peer *billingtest.EPayV2, mode, endpoint string) string {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(peer.Config(mode, "alipay")), &config); err != nil {
		t.Fatal(err)
	}
	config["url"] = endpoint
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func relativeEPayV2Checkout() Checkout {
	return Checkout{
		Order:     &order.Order{OrderNo: "order-1", PaymentAmount: 1000, PaymentCurrency: "CNY"},
		Charge:    Charge{Amount: 1000, Currency: "CNY"},
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

func TestEPayV2SiteRelativePageKeepsTheBrowserOnTheCurrentHost(t *testing.T) {
	peer, paths := newRelativeEPayV2(t)
	gw, err := NewRegistry(WithHTTPClient(peer.Server.Client())).Open(&payment.Payment{
		Platform: "EPay", Domain: peer.Server.URL + "/", Config: relativeEPayV2Config(t, peer, "page", "/pay"),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := gw.StartPayment(context.Background(), relativeEPayV2Checkout())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.CheckoutUrl, "/pay/api/pay/submit?") {
		t.Fatalf("checkout URL %q is not site-relative", resp.CheckoutUrl)
	}
	params, err := url.ParseQuery(strings.TrimPrefix(resp.CheckoutUrl, "/pay/api/pay/submit?"))
	if err != nil || params.Get("out_trade_no") != "order-1" || params.Get("sign_type") != "RSA" || params.Get("sign") == "" {
		t.Fatalf("checkout parameters %v (%v)", params, err)
	}
	result, err := gw.(*epayGateway).queryOrder(context.Background(), "order-1")
	if err != nil || !result.Paid {
		t.Fatalf("query result %+v (%v)", result, err)
	}
	if got := paths.list(); len(got) != 1 || got[0] != "/pay/api/pay/query" {
		t.Fatalf("server-side requests went to %v", got)
	}
}

func TestEPayV2SiteRelativeAPICheckoutUsesTheMethodDomain(t *testing.T) {
	peer, paths := newRelativeEPayV2(t)
	gw, err := NewRegistry(WithHTTPClient(peer.Server.Client())).Open(&payment.Payment{
		Platform: "EPay", Domain: peer.Server.URL, Config: relativeEPayV2Config(t, peer, "api", "/pay"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.7", "test-agent"))
	resp, err := gw.StartPayment(ctx, relativeEPayV2Checkout())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Type != "url" || resp.CheckoutUrl != "https://pay.example/checkout" {
		t.Fatalf("API checkout %+v", resp)
	}
	if got := paths.list(); len(got) != 1 || got[0] != "/pay/api/pay/create" {
		t.Fatalf("server-side requests went to %v", got)
	}
}

func TestEPayV2DeploymentPathAndSchemeRelativeURL(t *testing.T) {
	peer, paths := newRelativeEPayV2(t)
	registry := NewRegistry(WithHTTPClient(peer.Server.Client()))
	host := strings.TrimPrefix(peer.Server.URL, "https:")
	for _, tc := range []struct{ endpoint, prefix string }{
		{peer.Server.URL + "/pay/", peer.Server.URL + "/pay/api/pay/submit?"},
		{host + "/pay", host + "/pay/api/pay/submit?"},
	} {
		gw, err := registry.Open(&payment.Payment{Platform: "EPay", Config: relativeEPayV2Config(t, peer, "page", tc.endpoint)})
		if err != nil {
			t.Fatalf("%s: %v", tc.endpoint, err)
		}
		resp, err := gw.StartPayment(context.Background(), relativeEPayV2Checkout())
		if err != nil || !strings.HasPrefix(resp.CheckoutUrl, tc.prefix) {
			t.Fatalf("%s: checkout %q (%v)", tc.endpoint, resp.CheckoutUrl, err)
		}
		if _, err := gw.(*epayGateway).queryOrder(context.Background(), "order-1"); err != nil {
			t.Fatalf("%s: query %v", tc.endpoint, err)
		}
	}
	if got := paths.list(); len(got) != 2 || got[0] != "/pay/api/pay/query" || got[1] != "/pay/api/pay/query" {
		t.Fatalf("server-side requests went to %v", got)
	}
}

func TestEPayV2SiteRelativeURLRequiresTheMethodDomain(t *testing.T) {
	peer, _ := newRelativeEPayV2(t)
	if _, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Config: relativeEPayV2Config(t, peer, "page", "/pay")}); err == nil {
		t.Fatal("a site-relative V2 URL opened without a domain to reach it from the server")
	}
	if _, err := NewRegistry().Open(&payment.Payment{Platform: "EPay", Domain: "http://api.example", Config: relativeEPayV2Config(t, peer, "page", "/pay")}); err == nil {
		t.Fatal("a site-relative V2 URL resolved against an HTTP domain")
	}
}

func TestEPayV2RelativeConfigurationSurvivesNormalization(t *testing.T) {
	peer, _ := newRelativeEPayV2(t)
	registry := NewRegistry()
	normalize := func(endpoint string) (string, error) {
		var config map[string]any
		if err := json.Unmarshal([]byte(relativeEPayV2Config(t, peer, "page", endpoint)), &config); err != nil {
			t.Fatal(err)
		}
		return registry.NormalizeConfig("EPay", config)
	}
	for _, endpoint := range []string{"/pay", "/pay/", "//pay.example/pay", "https://pay.example/pay"} {
		normalized, err := normalize(endpoint)
		if err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		var config payment.EPayConfig
		if err := json.Unmarshal([]byte(normalized), &config); err != nil || config.Url != endpoint {
			t.Fatalf("%s: normalized address changed to %q (%v)", endpoint, config.Url, err)
		}
	}
	for _, endpoint := range []string{"/pay?key=x", "/pay#fragment", "///host", "/pay/../admin", "/pay/%2e%2e", "http://pay.example/pay"} {
		if _, err := normalize(endpoint); err == nil {
			t.Fatalf("invalid V2 address accepted: %q", endpoint)
		}
	}
}
