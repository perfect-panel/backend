package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

func newEPayReplayFixture(t *testing.T, version string) (*portalFixture, string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	var f *portalFixture
	var peer *billingtest.EPayV2
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if got := f.h.ReloadOrder("api-order"); got.PaymentSession != order.PaymentSessionStarting || got.PaymentCurrency != "CNY" || got.PaymentAmount != 1000 {
			t.Error("API called before the payment expectation and create fence were persisted")
		}
		if r.PostForm.Get("out_trade_no") != "api-order" || r.PostForm.Get("money") != "10.00" || r.PostForm.Get("clientip") != "127.0.0.10" {
			t.Error("API request lost the server-owned order, amount or resolved client IP")
		}
		if version == "v1" {
			if r.URL.Path != "/mapi.php" || r.PostForm.Get("device") != "jump" {
				t.Error("unexpected V1 create")
			}
			_, _ = fmt.Fprint(w, `{"code":1,"trade_no":"trade-1","payurl":"https://pay.example/checkout"}`)
		} else {
			if r.URL.Path != "/api/pay/create" || r.PostForm.Get("method") != "jump" {
				t.Error("unexpected V2 create")
			}
			peer.Reply(w, map[string]any{"code": 0, "trade_no": "trade-1", "pay_type": "qrcode", "pay_info": "weixin://payment"})
		}
	}
	var config string
	var client *http.Client
	if version == "v1" {
		server := httptest.NewTLSServer(http.HandlerFunc(handler))
		t.Cleanup(server.Close)
		client = server.Client()
		config = fmt.Sprintf(`{"pid":"1001","url":%q,"key":"secret","type":"alipay","checkout_mode":"api"}`, server.URL)
	} else {
		peer = billingtest.NewEPayV2(t, handler)
		client, config = peer.Server.Client(), peer.Config("api", "alipay")
	}
	f = newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithHTTPClient(client)) })
	return f, config, &calls
}

func TestEPayAPICheckoutPersistsAndReplaysBothProtocols(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			f, config, calls := newEPayReplayFixture(t, version)
			buyer, ctx := f.buyer(0, 0)
			ctx = requestmeta.With(ctx, requestmeta.New("127.0.0.10", "test"))
			method := f.h.Payment("EPay", config)
			f.pendingOrder("api-order", buyer.Id, 1000, method)
			first, err := f.checkout(ctx, "api-order", "")
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				result, err := f.checkout(ctx, "api-order", "")
				if err != nil || result.Type != first.Type || result.CheckoutUrl != first.CheckoutUrl {
					t.Fatalf("replay: %v", err)
				}
			}
			saved := f.h.ReloadOrder("api-order")
			if calls.Load() != 1 || saved.TradeNo != "trade-1" || saved.Status != order.StatusPending || saved.PaymentSession == order.PaymentSessionStarting {
				t.Fatalf("create calls=%d trade=%q status=%d", calls.Load(), saved.TradeNo, saved.Status)
			}
		})
	}
}

func TestEPayAPICheckoutDoesNotRetryUnknownCreate(t *testing.T) {
	var calls atomic.Int32
	peer := billingtest.NewEPayV2(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	f := newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithHTTPClient(peer.Server.Client())) })
	buyer, ctx := f.buyer(0, 0)
	method := f.h.Payment("EPay", peer.Config("api", "alipay"))
	f.pendingOrder("unknown", buyer.Id, 1000, method)
	// Invalid local inputs must not consume the one remote create attempt.
	if _, err := f.checkout(ctx, "unknown", ""); err == nil {
		t.Fatal("create without client IP accepted")
	}
	if calls.Load() != 0 || f.h.ReloadOrder("unknown").PaymentSession != "" {
		t.Fatal("local validation consumed a remote attempt")
	}
	ctx = requestmeta.With(ctx, requestmeta.New("127.0.0.1", "test"))
	for range 3 {
		if _, err := f.checkout(ctx, "unknown", ""); err == nil {
			t.Fatal("unknown checkout returned success")
		}
	}
	if calls.Load() != 1 || f.h.ReloadOrder("unknown").PaymentSession != order.PaymentSessionStarting {
		t.Fatal("unknown create was retried or its durable fence was lost")
	}
}

func TestEPayAPICheckoutConcurrentRequestsCreateOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var peer *billingtest.EPayV2
	peer = billingtest.NewEPayV2(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		peer.Reply(w, map[string]any{"code": 0, "trade_no": "trade-1", "pay_type": "jump", "pay_info": "https://pay.example/checkout"})
	})
	f := newPortalFixture(t, func(d *Deps) { d.Gateways = gateway.NewRegistry(gateway.WithHTTPClient(peer.Server.Client())) })
	buyer, ctx := f.buyer(0, 0)
	ctx = requestmeta.With(ctx, requestmeta.New("127.0.0.1", "test"))
	method := f.h.Payment("EPay", peer.Config("api", "alipay"))
	f.pendingOrder("concurrent", buyer.Id, 1000, method)
	result := make(chan error, 1)
	go func() {
		_, err := f.checkout(ctx, "concurrent", "")
		result <- err
	}()
	select {
	case <-entered:
	case err := <-result:
		close(release)
		t.Fatalf("checkout failed before reaching gateway: %v", err)
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("checkout did not reach gateway")
	}
	_, secondErr := f.checkout(ctx, "concurrent", "")
	close(release)
	if err := <-result; err != nil || secondErr == nil {
		t.Fatalf("first=%v concurrent=%v", err, secondErr)
	}
	if _, err := f.checkout(ctx, "concurrent", ""); err != nil || calls.Load() != 1 {
		t.Fatalf("replay=%v calls=%d", err, calls.Load())
	}
}

func TestPaymentSessionCannotCompleteOnClosedOrDifferentlyPaidOrder(t *testing.T) {
	f := newPortalFixture(t)
	for _, status := range []uint8{order.StatusClosed, order.StatusPaid, order.StatusFinished} {
		o := f.h.Order(&order.Order{
			OrderNo: fmt.Sprint(status), Status: status, TradeNo: "existing",
			PaymentSession: order.PaymentSessionStarting, PaymentAmount: 1000, PaymentCurrency: "CNY",
		})
		if ok, err := f.h.Store.Order().CompletePaymentSession(context.Background(), o.OrderNo, "new-trade", `{"state":"ready"}`); err != nil || ok {
			t.Fatalf("status %d: complete=%t err=%v", status, ok, err)
		}
		if got := f.h.ReloadOrder(o.OrderNo); got.Status != status || got.TradeNo != "existing" {
			t.Fatal("session completion overwrote a terminal or paid order")
		}
	}
}
