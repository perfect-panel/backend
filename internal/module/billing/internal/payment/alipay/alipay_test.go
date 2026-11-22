package alipay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
)

// The trade must ask for exactly the amount the order expects; the float
// formatter this replaced asked 0.56 for an expectation of 0.57.
func TestPreCreateRequestSendsExactAmounts(t *testing.T) {
	client := &Client{Config: Config{InvoiceName: "Plan", NotifyURL: "https://merchant.example/notify"}}
	for amount := int64(1); amount <= 100000; amount++ {
		request := client.preCreateRequest(Order{OrderNo: "order-1", Amount: amount})
		sent, err := payment.ParseAmount(request.TotalAmount)
		if err != nil || sent != amount {
			t.Fatalf("amount %d was sent as %q", amount, request.TotalAmount)
		}
	}
}

// The signed gateway receives exactly the expected amount.
func TestPreCreateTradeAgainstGateway(t *testing.T) {
	gateway := billingtest.NewFakeAlipay(t, func(method string, _ int, biz map[string]any) billingtest.AlipayAnswer {
		if method != "alipay.trade.precreate" {
			t.Errorf("unexpected method %q", method)
		}
		return billingtest.AlipayAnswer{Signed: true, Biz: fmt.Sprintf(
			`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.alipay.com/test"}`, biz["out_trade_no"])}
	})
	private, public := billingtest.AlipayKeys(t)
	client, err := NewClient(Config{
		AppId: "2021000000000000", PrivateKey: private, PublicKey: public, Sandbox: true, Gateway: gateway.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for amount, want := range map[int64]string{58: "0.58", 1990: "19.90", 100: "1.00"} {
		qr, err := client.PreCreateTrade(context.Background(), Order{OrderNo: "order-1", Amount: amount})
		if err != nil || qr != "https://qr.alipay.com/test" {
			t.Fatalf("PreCreateTrade(%d) = (%q, %v)", amount, qr, err)
		}
		if sent := gateway.LastBiz("alipay.trade.precreate")["total_amount"]; sent != want {
			t.Fatalf("amount %d was sent as %v, want %q", amount, sent, want)
		}
	}
}

// The trade expires at the order's own deadline, sent as the absolute time
// the gateway reads in UTC+8; only a trade without a deadline falls back to
// the relative window counted from the pre-creation.
func TestPreCreateRequestSendsTheAbsoluteExpiry(t *testing.T) {
	client := &Client{Config: Config{InvoiceName: "Plan"}}
	expireAt := time.Date(2026, 9, 28, 16, 30, 0, 0, time.UTC) // 00:30 the next day in UTC+8
	request := client.preCreateRequest(Order{OrderNo: "order-1", Amount: 100, ExpireAt: expireAt})
	if request.TimeExpire != "2026-09-29 00:30:00" || request.TimeoutExpress != "" {
		t.Fatalf("request = time_expire %q timeout_express %q, want the absolute expiry in UTC+8 only", request.TimeExpire, request.TimeoutExpress)
	}
	if got := FormatTimeExpire(expireAt.In(time.FixedZone("EST", -5*60*60))); got != "2026-09-29 00:30:00" {
		t.Fatalf("FormatTimeExpire = %q, want the instant rendered in UTC+8 whatever its zone", got)
	}
	fallback := client.preCreateRequest(Order{OrderNo: "order-1", Amount: 100})
	if fallback.TimeExpire != "" || fallback.TimeoutExpress != fallbackTimeout {
		t.Fatalf("request without a deadline = time_expire %q timeout_express %q", fallback.TimeExpire, fallback.TimeoutExpress)
	}
}

func TestNewClientRejectsUnusableKeys(t *testing.T) {
	if _, err := NewClient(Config{AppId: "app", PrivateKey: "not a key", Sandbox: true}); err == nil {
		t.Fatal("an unparsable merchant key must be rejected")
	}
}
