package portal

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
)

func TestGMPayCashierCheckout(t *testing.T) {
	const ready = `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","amount":10,"currency":"CNY","actual_amount":0,"status":4,"payment_url":"https://cashier.example/pay/trade-1"}}`
	for name, response := range map[string]string{
		"ready":          ready,
		"lost response":  "",
		"wrong amount":   strings.Replace(ready, `"amount":10`, `"amount":1`, 1),
		"wrong currency": strings.Replace(ready, "CNY", "USD", 1),
		"wrong order":    strings.Replace(ready, "order-1", "other", 1),
		"invalid link":   strings.Replace(ready, "https://cashier.example/pay/trade-1", "javascript:void(0)", 1),
		"business error": `{"status_code":10002}`,
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			var f *portalFixture
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				checkGMPayCreateRequest(t, r, f)
				if response == "" {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				_, _ = io.WriteString(w, response)
			}))
			defer s.Close()
			f = newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithHTTPClient(s.Client())) })
			buyer, ctx := f.buyer(0, 0)
			// Stale configuration must still create a checkout with no asset selected.
			method := f.h.Payment("GMPay", fmt.Sprintf(`{"url":%q,"pid":"1000","secret_key":"sample","network":"tron","token":"usdt"}`, s.URL+"/gm"))
			f.pendingOrder("order-1", buyer.Id, 1000, method)
			for range 2 {
				result, err := f.checkout(ctx, "order-1", "")
				if name == "ready" {
					if err != nil || result.Type != "url" || result.CheckoutUrl != "https://cashier.example/pay/trade-1" {
						t.Fatalf("checkout = %+v, %v", result, err)
					}
				} else if err == nil {
					t.Fatal("ambiguous or invalid create accepted")
				}
			}
			got := f.h.ReloadOrder("order-1")
			if calls.Load() != 1 || got.Status != order.StatusPending {
				t.Fatalf("create calls=%d, status=%d", calls.Load(), got.Status)
			}
			if name == "ready" {
				if got.TradeNo != "trade-1" || got.PaymentSession == order.PaymentSessionStarting {
					t.Fatal("cashier entry not persisted")
				}
			} else if got.TradeNo != "" || got.PaymentSession != order.PaymentSessionStarting {
				t.Fatal("ambiguous create lost its fence")
			}
		})
	}
}

func checkGMPayCreateRequest(t *testing.T, r *http.Request, f *portalFixture) {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.Path != "/gm/payments/gmpay/v1/order/create-transaction" {
		t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
	}
	if err := r.ParseForm(); err != nil {
		t.Error(err)
	}
	v := r.PostForm
	if v.Get("pid") != "1000" || v.Get("order_id") != "order-1" || v.Get("amount") != "10.00" || v.Get("currency") != "cny" || v.Has("network") || v.Has("token") || v.Has("payment_type") || v.Get("signature") == "" {
		t.Errorf("unexpected cashier form %v", v)
	}
	o := f.h.ReloadOrder("order-1")
	if o.PaymentSession != order.PaymentSessionStarting || o.PaymentAmount != 1000 {
		t.Error("order and attempt must be recorded before sending")
	}
}

func TestGMPayConcurrentCheckoutCreatesOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		_, _ = io.WriteString(w, `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","amount":10,"currency":"CNY","status":4,"payment_url":"https://cashier.example/pay/trade-1"}}`)
	}))
	defer s.Close()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	f := newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithHTTPClient(s.Client())) })
	buyer, ctx := f.buyer(0, 0)
	method := f.h.Payment("GMPay", fmt.Sprintf(`{"url":%q,"pid":"1000","secret_key":"test"}`, s.URL))
	f.pendingOrder("order-1", buyer.Id, 1000, method)
	done := make(chan error, 1)
	go func() { _, err := f.checkout(ctx, "order-1", ""); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("checkout ended before create: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("create request did not arrive")
	}
	if _, err := f.checkout(ctx, "order-1", ""); err == nil {
		t.Error("concurrent checkout accepted an incomplete session")
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || f.h.ReloadOrder("order-1").TradeNo != "trade-1" {
		t.Fatal("concurrent checkout did not bind exactly one trade")
	}
}
