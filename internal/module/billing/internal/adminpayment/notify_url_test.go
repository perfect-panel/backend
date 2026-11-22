package adminpayment

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

// The admin list shows the callback URL checkout actually gives the
// gateway: the payment domain, else a reachable configured host, else the
// site host. The 0.0.0.0 listen address used to be shown as https://0.0.0.0.
func TestListPaymentNotifyURLs(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		domain   string
		siteHost string
		want     string
	}{
		{"stripe domain", "Stripe", "https://pay.example.test/", "panel.example.test", "https://pay.example.test/v1/notify/Stripe/test-token"},
		{"cryptomus domain", "Cryptomus", "https://pay.example.test", "panel.example.test", "https://pay.example.test/v1/notify/Cryptomus/test-token"},
		{"configured base path", "EPay", "https://pay.example.test/custom/", "panel.example.test", "https://pay.example.test/custom/v1/notify/EPay/test-token"},
		{"site host fallback", "AlipayF2F", "", "www.example.test/", "https://www.example.test/v1/notify/AlipayF2F/test-token"},
		{"not configured", "EPay", "", "", ""},
		{"balance has no callback", "balance", "https://pay.example.test", "panel.example.test", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := true
			repo := newMemoryPayments(&payment.Payment{
				Id: 1, Platform: tt.platform, Domain: tt.domain, Token: "test-token", Enable: &enabled,
			})
			siteHost := tt.siteHost
			svc := NewService(Deps{Payments: repo, SiteHost: func() string { return siteHost }})
			resp, err := svc.List(context.Background(), &dto.GetPaymentMethodListRequest{Page: 1, Size: 10})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Total != 1 || len(resp.List) != 1 {
				t.Fatalf("unexpected payment list: %+v", resp)
			}
			if got := resp.List[0].NotifyURL; got != tt.want {
				t.Fatalf("NotifyURL = %q, want %q", got, tt.want)
			}
		})
	}
}
