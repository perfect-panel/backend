package callbacks

import (
	"net/http"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
)

func TestEPayV2CallbackRequiresMatchingSignedQueryAndSettlesIdempotently(t *testing.T) {
	for _, scenario := range []string{"valid", "amount mismatch", "trade mismatch", "unsigned success", "missing query"} {
		t.Run(scenario, func(t *testing.T) {
			var peer *billingtest.EPayV2
			peer = billingtest.NewEPayV2(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/pay/query" {
					t.Error("V2 callback tried a legacy protocol")
				}
				if scenario == "missing query" {
					http.NotFound(w, r)
					return
				}
				if scenario == "unsigned success" {
					_, _ = w.Write([]byte(`{"code":0,"status":1,"pid":"1001","trade_no":"trade-1","out_trade_no":"order-1","type":"alipay","money":"10.00"}`))
					return
				}
				fields := map[string]any{"code": 0, "status": 1, "pid": "1001", "trade_no": "trade-1", "out_trade_no": "order-1", "type": "alipay", "money": "10.00"}
				if scenario == "amount mismatch" {
					fields["money"] = "1.00"
				}
				if scenario == "trade mismatch" {
					fields["trade_no"] = "another-trade"
				}
				peer.Reply(w, fields)
			})
			h := billingtest.New(t)
			method := h.Payment("EPay", peer.Config("page", "alipay"))
			pending := pendingOrder(method, 1000, "CNY")
			pending.PaymentSession = order.PaymentSessionStarting
			h.Order(pending)
			queue := &fakeActivationQueue{}
			svc := NewService(h.Store.Order(), queue, gateway.NewRegistry(gateway.WithHTTPClient(peer.Server.Client())))
			params := peer.Sign(map[string]string{"out_trade_no": "order-1", "trade_no": "trade-1", "type": "alipay", "money": "10.00", "trade_status": "TRADE_SUCCESS"})
			err := svc.Notify(paymentContext(method), gateway.Notification{Params: params})
			if scenario != "valid" {
				if err == nil || h.ReloadOrder("order-1").Status != order.StatusPending || len(queue.enqueued) != 0 {
					t.Fatalf("invalid query settled: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			firstVersion := h.ReloadOrder("order-1").StateVersion
			if err := svc.Notify(paymentContext(method), gateway.Notification{Params: params}); err != nil {
				t.Fatal(err)
			}
			paid := h.ReloadOrder("order-1")
			if paid.Status != order.StatusPaid || paid.TradeNo != "trade-1" || paid.StateVersion != firstVersion {
				t.Fatal("duplicate callback repeated or lost the paid transition")
			}
		})
	}
}
