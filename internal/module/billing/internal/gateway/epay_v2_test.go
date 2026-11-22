package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
)

func TestEPayProtocolConfiguration(t *testing.T) {
	peer := billingtest.NewEPayV2(t, http.NotFound)
	registry := NewRegistry(WithHTTPClient(peer.Server.Client()))
	for _, raw := range []string{
		`{"pid":"1001","url":"https://pay.example/legacy/submit.php","key":"k","type":""}`,
		`{"pid":"1001","url":"https://pay.example","key":"k","type":"alipay","version":"v1","checkout_mode":"api"}`,
		peer.Config("page", ""), peer.Config("api", "wxpay"),
	} {
		var config map[string]any
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}
		normalized, err := registry.NormalizeConfig("EPay", config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Open(&payment.Payment{Platform: "EPay", Config: normalized}); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []map[string]any{
		{"version": "v3"}, {"checkout_mode": "wrong"}, {"url": "http://pay.example"},
		{"public_key": "invalid"}, {"private_key": ""}, {"key": "v1-key"}, {"type": "bank"},
		{"checkout_mode": "api", "type": ""}, {"version": "v1"},
	} {
		var config map[string]any
		if err := json.Unmarshal([]byte(peer.Config("page", "alipay")), &config); err != nil {
			t.Fatal(err)
		}
		for key, value := range change {
			config[key] = value
		}
		if _, err := registry.NormalizeConfig("EPay", config); err == nil {
			t.Fatal("invalid protocol configuration accepted")
		}
	}
}

func TestEPayV2PageAndNotification(t *testing.T) {
	var peer *billingtest.EPayV2
	peer = billingtest.NewEPayV2(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pay/query" || r.PostForm.Get("out_trade_no") != "order-1" {
			t.Error("unexpected EPay V2 query")
		}
		peer.Reply(w, epayV2Query(1))
	})
	gw, err := NewRegistry(WithHTTPClient(peer.Server.Client())).Open(&payment.Payment{Platform: "EPay", Config: peer.Config("page", "alipay")})
	if err != nil {
		t.Fatal(err)
	}
	o := &order.Order{OrderNo: "order-1", PaymentAmount: 1000, PaymentCurrency: "CNY"}
	entry, err := gw.StartPayment(context.Background(), Checkout{
		Order: o, Charge: Charge{Amount: 1000, Currency: "CNY"}, Subject: "会员 A&B",
		NotifyURL: "https://panel.example/notify", ReturnURL: "https://panel.example/return",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(entry.CheckoutUrl)
	if err != nil || u.Path != "/api/pay/submit" || u.Query().Get("sign_type") != "RSA" || u.Query().Get("money") != "10.00" || u.Query().Get("name") != "会员 A&B" || u.Query().Has("key") {
		t.Fatal("invalid V2 page payment")
	}
	params := map[string]string{"out_trade_no": "order-1", "trade_no": "trade-1", "type": "alipay", "money": "10.00", "trade_status": "TRADE_SUCCESS"}
	notice, err := gw.ParseCallback(context.Background(), Notification{Params: peer.Sign(params)})
	if err != nil || notice.Amount != 1000 || !notice.Paid {
		t.Fatalf("ParseCallback: %v", err)
	}
	if err := gw.ConfirmPayment(context.Background(), o, notice); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]map[string]string{
		"merchant": {"pid": "other"}, "type": {"type": "wxpay"}, "status": {"trade_status": "WAIT_BUYER_PAY"},
		"expired": {"timestamp": strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)},
	} {
		t.Run(name, func(t *testing.T) {
			values := make(map[string]string, len(params))
			for key, value := range params {
				values[key] = value
			}
			for key, value := range change {
				values[key] = value
			}
			if _, err := gw.ParseCallback(context.Background(), Notification{Params: peer.Sign(values)}); err == nil {
				t.Fatal("invalid callback accepted")
			}
		})
	}
	for _, mutate := range []func(map[string]string){
		func(v map[string]string) { v["money"] = "0.01" },
		func(v map[string]string) { v["sign_type"] = "MD5" },
	} {
		values := peer.Sign(params)
		mutate(values)
		if _, err := gw.ParseCallback(context.Background(), Notification{Params: values}); err == nil {
			t.Fatal("tampered or downgraded callback accepted")
		}
	}
}

func epayV2Query(status int) map[string]any {
	return map[string]any{"code": 0, "trade_no": "trade-1", "out_trade_no": "order-1", "type": "alipay", "money": "10.00", "status": status}
}

type epayReconcileCase struct {
	name          string
	status        int
	closeFails    bool
	paidAfterRace bool
	queryFails    bool
	mismatch      bool
	partialRefund bool
	wantPaid      bool
	wantError     bool
	wantClose     int32
}

func newEPayReconcilePeer(t *testing.T, tt epayReconcileCase) (*billingtest.EPayV2, *atomic.Int32) {
	t.Helper()
	var queries, closes atomic.Int32
	var peer *billingtest.EPayV2
	peer = billingtest.NewEPayV2(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pay/query":
			count := queries.Add(1)
			if tt.queryFails {
				http.NotFound(w, r)
				return
			}
			status := tt.status
			if count > 1 && tt.paidAfterRace {
				status = 1
			}
			response := epayV2Query(status)
			if tt.mismatch {
				response["money"] = "9.00"
			}
			if tt.partialRefund {
				response["refundmoney"] = "1.00"
			}
			peer.Reply(w, response)
		case "/api/pay/close":
			closes.Add(1)
			if r.PostForm.Get("trade_no") != "trade-1" {
				t.Error("close did not use the verified platform trade")
			}
			if tt.closeFails {
				peer.Reply(w, map[string]any{"code": -1, "msg": "cannot close"})
			} else {
				peer.Reply(w, map[string]any{"code": 0})
			}
		default:
			t.Error("V2 fell back to another protocol")
			http.NotFound(w, r)
		}
	})
	return peer, &closes
}

func TestEPayV2CancellationReconcilesBeforeClosing(t *testing.T) {
	for _, tt := range []epayReconcileCase{
		{name: "paid", status: 1, wantPaid: true},
		{name: "unpaid closes remotely", status: 0, wantClose: 1},
		{name: "payment wins close race", status: 0, closeFails: true, paidAfterRace: true, wantPaid: true, wantClose: 1},
		{name: "failed close stays pending", status: 0, closeFails: true, wantError: true, wantClose: 1},
		{name: "unsupported query cannot downgrade", queryFails: true, wantError: true},
		{name: "frozen remains pending", status: 3, wantError: true},
		{name: "refunded remains pending", status: 2, wantError: true},
		{name: "partial refund remains pending", status: 1, partialRefund: true, wantError: true},
		{name: "wrong amount never closes", status: 0, mismatch: true, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			peer, closes := newEPayReconcilePeer(t, tt)
			gw, err := NewRegistry(WithHTTPClient(peer.Server.Client())).Open(&payment.Payment{Platform: "EPay", Config: peer.Config("page", "")})
			if err != nil {
				t.Fatal(err)
			}
			verdict, err := gw.Reconcile(context.Background(), CloseRequest{
				Order: &order.Order{OrderNo: "order-1", PaymentCurrency: "CNY", PaymentAmount: 1000}, Explicit: true,
			})
			if (err != nil) != tt.wantError || (verdict.TradeNo != "") != tt.wantPaid || closes.Load() != tt.wantClose {
				t.Fatalf("Reconcile: trade=%q err=%v closeCalls=%d", verdict.TradeNo, err, closes.Load())
			}
			if !tt.wantError && !tt.wantPaid && !verdict.RequireStableCheckout {
				t.Fatal("remote close must recheck the local checkout snapshot")
			}
			if tt.queryFails && !errors.Is(err, ErrUnconfirmed) {
				t.Fatal("query failure must remain unconfirmed")
			}
		})
	}
}
