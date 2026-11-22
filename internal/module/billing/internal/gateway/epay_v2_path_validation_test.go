package gateway

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

// Saving and opening a configuration must reject paths that would turn decoded
// delimiters into a query/fragment, or leave two leading slashes in a page URL.
func TestEPayV2RejectsAmbiguousDeploymentPaths(t *testing.T) {
	peer, _ := newRelativeEPayV2(t)
	registry := NewRegistry()
	for _, mode := range []string{"page", "api"} {
		for _, endpoint := range []string{
			"/pay%23checkout", "/pay%3Froute=wrong", "/pay%20space", "/pay%0Aline",
			"//pay.example/pay%23checkout", "https://pay.example/pay%3Froute=wrong",
			"//pay.example//", "https://pay.example//",
		} {
			t.Run(mode+"/"+endpoint, func(t *testing.T) {
				raw := relativeEPayV2Config(t, peer, mode, endpoint)
				var config map[string]any
				if err := json.Unmarshal([]byte(raw), &config); err != nil {
					t.Fatal(err)
				}
				if _, err := registry.NormalizeConfig("EPay", config); err == nil {
					t.Error("ambiguous address passed configuration validation")
				}
				if _, err := registry.Open(&payment.Payment{Platform: "EPay", Domain: peer.Server.URL, Config: raw}); err == nil {
					t.Error("ambiguous address opened a gateway")
				}
			})
		}
	}
}

// Domain is combined with a stored site-relative path only when opening the
// gateway. A malformed Domain must not produce //api/... in the browser.
func TestEPayV2SiteRelativeRootCannotBecomeAnAuthority(t *testing.T) {
	peer, _ := newRelativeEPayV2(t)
	registry := NewRegistry()
	for _, domain := range []string{peer.Server.URL, peer.Server.URL + "/"} {
		gw, err := registry.Open(&payment.Payment{
			Platform: "EPay", Domain: domain, Config: relativeEPayV2Config(t, peer, "page", "/"),
		})
		if err != nil {
			t.Fatal(err)
		}
		response, err := gw.StartPayment(context.Background(), relativeEPayV2Checkout())
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(response.CheckoutUrl)
		if err != nil || parsed.Host != "" || !strings.HasPrefix(response.CheckoutUrl, "/api/pay/submit?") ||
			parsed.Query().Get("money") != "10.00" || parsed.Query().Get("pid") != "1001" || parsed.Query().Get("sign") == "" {
			t.Fatal("root checkout changed its host, path or signed payment fields")
		}
	}
	for _, mode := range []string{"page", "api"} {
		for _, suffix := range []string{"//", "///"} {
			if _, err := registry.Open(&payment.Payment{
				Platform: "EPay", Domain: peer.Server.URL + suffix, Config: relativeEPayV2Config(t, peer, mode, "/"),
			}); err == nil {
				t.Errorf("%s mode accepted a Domain ending in %q for a root deployment", mode, suffix)
			}
		}
	}
}
