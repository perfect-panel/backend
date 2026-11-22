package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

func TestGMPayReconcileBeforeClose(t *testing.T) {
	for _, tt := range []struct {
		name           string
		trade, session string
		status         int
		currency       string
		paid, close    bool
	}{
		{name: "never sent", close: true},
		{name: "create timed out", session: order.PaymentSessionStarting},
		{name: "waiting", trade: "trade", status: 1, currency: "USD"},
		{name: "paid", trade: "trade", status: 2, currency: "USD", paid: true},
		{name: "expired", trade: "trade", status: 3, currency: "USD", close: true},
		{name: "choosing a network", trade: "trade", status: 4, currency: "USD"},
		{name: "confirming", trade: "trade", status: 5, currency: "USD"},
		{name: "manual review", trade: "trade", status: 6, currency: "USD"},
		{name: "unknown", trade: "trade", status: 7, currency: "USD"},
		{name: "expired in wrong currency", trade: "trade", status: 3, currency: "CNY"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.trade == "" || r.URL.Path != "/pay/checkout-counter-resp/trade" {
					t.Errorf("unexpected query %s", r.URL)
				}
				_, _ = fmt.Fprintf(w, `{"status_code":200,"data":{"trade_id":"trade","payment_type":"gmpay","amount":12.34,"currency":%q,"status":%d}}`, tt.currency, tt.status)
			}))
			defer s.Close()
			g, err := NewRegistry(WithHTTPClient(s.Client())).Open(&paymentEntity.Payment{Platform: "GMPay", Config: fmt.Sprintf(`{"url":%q,"pid":"1000","secret_key":"key"}`, s.URL)})
			if err != nil {
				t.Fatal(err)
			}
			o := &order.Order{PaymentAmount: 1234, PaymentCurrency: "USD", TradeNo: tt.trade, PaymentSession: tt.session}
			for _, explicit := range []bool{false, true} {
				got, err := g.Reconcile(t.Context(), CloseRequest{Order: o, Explicit: explicit})
				if !tt.paid && !tt.close {
					if !errors.Is(err, ErrUnconfirmed) {
						t.Fatalf("wanted pending, got %+v, %v", got, err)
					}
				} else if err != nil || (tt.paid && got.TradeNo != tt.trade) || got.RequireStableCheckout != tt.close {
					t.Fatalf("wrong close result %+v, %v", got, err)
				}
			}
		})
	}
}
