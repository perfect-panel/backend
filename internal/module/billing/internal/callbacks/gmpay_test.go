package callbacks

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
)

func TestGMPayCallbackSettlement(t *testing.T) {
	for _, scenario := range []string{
		"paid", "lost create response", "activation retry", "no create attempt",
		"invalid signature", "wrong method", "bound to another trade", "signed amount mismatch",
		"query amount mismatch", "wrong fiat", "wrong trade", "wrong payment type", "not confirmed", "query unavailable",
	} {
		t.Run(scenario, func(t *testing.T) {
			var reads atomic.Int32
			s := gmpayDetailServer(t, scenario, &reads)
			defer s.Close()
			h := billingtest.New(t)
			method := h.Payment("GMPay", fmt.Sprintf(`{"url":%q,"pid":"1000","secret_key":"epusdt_secret_key"}`, s.URL))
			o := pendingOrder(method, 1000, "CNY")
			n := gmpayCallbackCase(scenario, o)
			h.Order(o)
			queue := &billingtest.Queue{}
			svc := NewService(h.Store.Order(), queue, gateway.NewRegistry(gateway.WithHTTPClient(s.Client())))
			ctx := paymentContext(method)
			deliver := func() error { return svc.Notify(ctx, n) }
			if scenario == "activation retry" {
				checkActivationRetry(t, h, queue, o.OrderNo, deliver)
				return
			}
			err := deliver()
			if scenario != "paid" && scenario != "lost create response" {
				if err == nil || h.ReloadOrder(o.OrderNo).Status != order.StatusPending || len(queue.Activations) > 0 {
					t.Fatalf("unexpected settlement: %v", err)
				}
				if (scenario == "invalid signature" || scenario == "wrong method" || scenario == "no create attempt") && reads.Load() != 0 {
					t.Error("unverified callback reached the gateway")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			paid := h.ReloadOrder(o.OrderNo)
			if paid.Status != order.StatusPaid || paid.TradeNo != "trade-1" || len(queue.Activations) != 1 || reads.Load() != 1 {
				t.Fatal("payment or activation missing")
			}
			if err := deliver(); err != nil || h.ReloadOrder(o.OrderNo).StateVersion != paid.StateVersion {
				t.Fatalf("redelivery repeated the payment: %v", err)
			}
		})
	}
}

func gmpayCallbackCase(scenario string, o *order.Order) gateway.Notification {
	// Signed independently, including the crypto amount and chain transaction.
	const callback = `{"amount":10.00,"actual_amount":1.3827,"block_transaction_id":"tx","order_id":"order-1","pid":"1000","receive_address":"TAddr","status":2,"token":"USDT","trade_id":"trade-1","signature":"1b97f2d0f257203ae5b7023494f15c2b93f3f744a87dbe5896dab3bec05c295d"}`
	o.TradeNo = "trade-1"
	n := gateway.Notification{HTTPMethod: http.MethodPost, Body: []byte(callback)}
	switch scenario {
	case "lost create response":
		o.TradeNo = ""
		o.PaymentSession = order.PaymentSessionStarting
	case "no create attempt":
		o.TradeNo = ""
	case "invalid signature":
		n.Body = []byte(strings.Replace(callback, "1.3827", "99", 1))
	case "wrong method":
		n.HTTPMethod = http.MethodGet
	case "bound to another trade":
		o.TradeNo = "other"
	case "signed amount mismatch":
		o.PaymentAmount = 2000
	}
	return n
}

func gmpayDetailServer(t *testing.T, scenario string, reads *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.Method != "GET" || r.URL.Path != "/pay/checkout-counter-resp/trade-1" {
			t.Errorf("unexpected query %s %s", r.Method, r.URL.Path)
		}
		amount, currency, id, typ, status := "10.0000", "CNY", "trade-1", "gmpay", 2
		switch scenario {
		case "query amount mismatch":
			amount = "1.3827"
		case "wrong fiat":
			currency = "USD"
		case "wrong trade":
			id = "other"
		case "wrong payment type":
			typ = "epay"
		case "not confirmed":
			status = 5
		case "query unavailable":
			w.WriteHeader(503)
			return
		}
		_, _ = fmt.Fprintf(w, `{"status_code":200,"data":{"trade_id":%q,"amount":%s,"currency":%q,"status":%d,"payment_type":%q}}`, id, amount, currency, status, typ)
	}))
}

func checkActivationRetry(t *testing.T, h *billingtest.Harness, queue *billingtest.Queue, orderNo string, deliver func() error) {
	t.Helper()
	queue.ActivationErr = errors.New("queue unavailable")
	err := deliver()
	paid := h.ReloadOrder(orderNo)
	if err == nil || paid.Status != order.StatusPaid || len(queue.Activations) != 0 {
		t.Fatal("failed activation was acknowledged")
	}
	queue.ActivationErr = nil
	if err := deliver(); err != nil || len(queue.Activations) != 1 || h.ReloadOrder(orderNo).StateVersion != paid.StateVersion {
		t.Fatalf("retry failed to recover activation: %v", err)
	}
}
