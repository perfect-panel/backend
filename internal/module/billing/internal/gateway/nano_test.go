package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	"github.com/perfect-panel/server/internal/module/billing/entity/payment"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment/nano"
)

const (
	nanoHashA = "A1B2C3D4E5F60718293A4B5C6D7E8F90A1B2C3D4E5F60718293A4B5C6D7E8F90"
	nanoHashB = "0F1E2D3C4B5A69788796A5B4C3D2E1F00F1E2D3C4B5A69788796A5B4C3D2E1F0"
)

func nanoAccount(t *testing.T, fill byte) string {
	t.Helper()
	a, err := nano.EncodeAccount(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// nanoNode answers block_info for the blocks it holds.
type nanoNode map[string]map[string]any

func (n nanoNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	_ = json.NewDecoder(r.Body).Decode(&req)
	b, ok := n[req["hash"]]
	if req["action"] != "block_info" || !ok {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Block not found"})
		return
	}
	_ = json.NewEncoder(w).Encode(b)
}

func nanoSend(payer, payTo, raw, rep string) map[string]any {
	return map[string]any{
		"block_account": payer, "amount": raw, "confirmed": "true", "subtype": "send",
		"contents": map[string]string{"type": "state", "representative": rep, "link_as_account": payTo},
	}
}

type nanoFixture struct {
	gw    Gateway
	node  nanoNode
	payTo string
	payer string
}

func newNanoFixture(t *testing.T) *nanoFixture {
	t.Helper()
	node := nanoNode{}
	srv := httptest.NewServer(node)
	t.Cleanup(srv.Close)
	payTo := nanoAccount(t, 0x22)
	config, _ := json.Marshal(map[string]string{
		"pay_to": payTo, "node_url": srv.URL, "currency": "usd", "xno_per_unit": "2.5",
	})
	gw, err := NewRegistry().Open(&payment.Payment{Platform: "Nano", Config: string(config)})
	if err != nil {
		t.Fatal(err)
	}
	return &nanoFixture{gw: gw, node: node, payTo: payTo, payer: nanoAccount(t, 0x11)}
}

// rawFor is the exact raw price of minor units at the fixture's rate.
func rawFor(t *testing.T, minor int64) string {
	t.Helper()
	rate, err := nano.ParseRate("2.5")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rate.Raw(minor)
	if err != nil {
		t.Fatal(err)
	}
	return raw.String()
}

func nanoCallbackBody(orderNo, block string) []byte {
	body, _ := json.Marshal(map[string]string{"order_no": orderNo, "block": block})
	return body
}

func TestNanoCheckoutNamesTheBoundExactPayment(t *testing.T) {
	f := newNanoFixture(t)
	o := &order.Order{OrderNo: "202611170001", PaymentAmount: 1990, PaymentCurrency: "USD"}
	if f.gw.ChargeCurrency() != "USD" || f.gw.Platform().String() != "Nano" || !f.gw.NeedsNotifyURL(o) {
		t.Fatalf("gateway = %s %s", f.gw.Platform(), f.gw.ChargeCurrency())
	}
	res, err := f.gw.StartPayment(context.Background(), Checkout{
		Order: o, Charge: Charge{Amount: 1990, Currency: "USD"}, NotifyURL: "https://pay.example.test/v1/notify/Nano/tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(res.CheckoutUrl)
	if err != nil || res.Type != "qr" || u.Scheme != "nano" || u.Opaque != f.payTo {
		t.Fatalf("checkout = %+v (%v)", res, err)
	}
	raw := rawFor(t, 1990)
	q := u.Query()
	if q.Get("amount") != raw || q.Get("representative") != nano.CommitmentAccount(o.OrderNo, f.payTo, raw) ||
		q.Get("callback") != "https://pay.example.test/v1/notify/Nano/tok" {
		t.Fatalf("checkout query = %v", q)
	}
	// The charge must be the recorded expectation.
	if _, err := f.gw.StartPayment(context.Background(), Checkout{Order: o, Charge: Charge{Amount: 1991, Currency: "USD"}}); err == nil {
		t.Fatal("a charge that is not the expectation started a payment")
	}
	if _, err := f.gw.StartPayment(context.Background(), Checkout{Order: o, Charge: Charge{Amount: 1990, Currency: "EUR"}}); err == nil {
		t.Fatal("a charge in another currency started a payment")
	}
}

func TestNanoSettlesOnlyTheBoundExactBlock(t *testing.T) {
	f := newNanoFixture(t)
	ctx := context.Background()
	o := &order.Order{OrderNo: "202611170001", PaymentAmount: 1990, PaymentCurrency: "USD", Status: order.StatusPending}
	raw := rawFor(t, 1990)
	rep := nano.CommitmentAccount(o.OrderNo, f.payTo, raw)
	f.node[nanoHashA] = nanoSend(f.payer, f.payTo, raw, rep)

	notice, err := f.gw.ParseCallback(ctx, Notification{Body: nanoCallbackBody(o.OrderNo, strings.ToLower(nanoHashA))})
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if notice.OrderNo != o.OrderNo || notice.TradeNo != nanoHashA || notice.Amount != 1990 || notice.Currency != "USD" || !notice.Paid {
		t.Fatalf("notice = %+v", notice)
	}
	if err := f.gw.CheckOrder(o, notice); err != nil {
		t.Fatalf("CheckOrder: %v", err)
	}
	if err := f.gw.ConfirmPayment(ctx, o, notice); err != nil {
		t.Fatalf("ConfirmPayment: %v", err)
	}

	// The same block cannot settle another order of the same price: its
	// representative commits to the first order.
	other := &order.Order{OrderNo: "202611170002", PaymentAmount: 1990, PaymentCurrency: "USD", Status: order.StatusPending}
	replay, err := f.gw.ParseCallback(ctx, Notification{Body: nanoCallbackBody(other.OrderNo, nanoHashA)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ConfirmPayment(ctx, other, replay); !errors.Is(err, nano.ErrNotBound) {
		t.Fatalf("block reused for another order: %v", err)
	}
	// An order settled with one block refuses every other block.
	o.TradeNo = nanoHashB
	if err := f.gw.CheckOrder(o, notice); err == nil {
		t.Fatal("a second block was accepted for a settled order")
	}
}

func TestNanoRefusesBlocksThatDoNotPay(t *testing.T) {
	f := newNanoFixture(t)
	ctx := context.Background()
	o := &order.Order{OrderNo: "202611170001", PaymentAmount: 1990, PaymentCurrency: "USD"}
	raw := rawFor(t, 1990)
	rep := nano.CommitmentAccount(o.OrderNo, f.payTo, raw)
	unconfirmed := nanoSend(f.payer, f.payTo, raw, rep)
	unconfirmed["confirmed"] = "false"

	for name, block := range map[string]map[string]any{
		"wrong destination": nanoSend(f.payer, nanoAccount(t, 0x33), raw, rep),
		"one raw over":      nanoSend(f.payer, f.payTo, raw[:len(raw)-1]+"1", rep),
		"unconfirmed":       unconfirmed,
	} {
		f.node[nanoHashA] = block
		if _, err := f.gw.ParseCallback(ctx, Notification{Body: nanoCallbackBody(o.OrderNo, nanoHashA)}); err == nil {
			t.Errorf("%s: callback accepted", name)
		}
	}
	// A whole minor unit short parses, but is not the order's price.
	f.node[nanoHashA] = nanoSend(f.payer, f.payTo, rawFor(t, 1989), nano.CommitmentAccount(o.OrderNo, f.payTo, rawFor(t, 1989)))
	notice, err := f.gw.ParseCallback(ctx, Notification{Body: nanoCallbackBody(o.OrderNo, nanoHashA)})
	if err != nil || notice.Amount == o.PaymentAmount {
		t.Fatalf("short block: notice = %+v, %v", notice, err)
	}
	if err := f.gw.ConfirmPayment(ctx, o, notice); !errors.Is(err, nano.ErrWrongAmount) {
		t.Fatalf("short block confirmed: %v", err)
	}
	for _, body := range []string{`not json`, `{"order_no":"","block":"` + nanoHashA + `"}`, `{"order_no":"o","block":"xyz"}`} {
		if _, err := f.gw.ParseCallback(ctx, Notification{Body: []byte(body)}); err == nil {
			t.Errorf("callback %s accepted", body)
		}
	}
	if _, err := f.gw.ParseCallback(ctx, Notification{Body: nanoCallbackBody(o.OrderNo, nanoHashB)}); !errors.Is(err, nano.ErrNotConfirmed) {
		t.Fatalf("unknown block: %v", err)
	}
}

func TestNanoReconcileKeepsAStartedCheckoutPending(t *testing.T) {
	f := newNanoFixture(t)
	ctx := context.Background()
	started := &order.Order{OrderNo: "o", PaymentAmount: 1990, PaymentCurrency: "USD", CreatedAt: time.Now()}
	if _, err := f.gw.Reconcile(ctx, CloseRequest{Order: started}); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("a young started checkout closed: %v", err)
	}
	if verdict, err := f.gw.Reconcile(ctx, CloseRequest{Order: started, Explicit: true}); err != nil || !verdict.RequireStableCheckout || verdict.TradeNo != "" {
		t.Fatalf("explicit close = (%+v, %v)", verdict, err)
	}
	started.CreatedAt = time.Now().Add(-order.UnpaidCloseAge - time.Minute)
	if verdict, err := f.gw.Reconcile(ctx, CloseRequest{Order: started}); err != nil || !verdict.RequireStableCheckout {
		t.Fatalf("abandoned checkout = (%+v, %v)", verdict, err)
	}
}

func TestNanoConfiguration(t *testing.T) {
	registry := NewRegistry()
	payTo := nanoAccount(t, 0x22)
	normalized, err := registry.NormalizeConfig("Nano", map[string]any{
		"pay_to": " " + payTo + " ", "node_url": " https://node.example/rpc ", "currency": " usd ", "xno_per_unit": " 2.5 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got payment.NanoConfig
	if err := got.Unmarshal([]byte(normalized)); err != nil || got != (payment.NanoConfig{
		PayTo: payTo, NodeURL: "https://node.example/rpc", Currency: "USD", XNOPerUnit: "2.5",
	}) {
		t.Fatalf("normalized = %s (%v)", normalized, err)
	}
	bad := map[string]map[string]any{
		"bad checksum": {"pay_to": payTo[:len(payTo)-1] + "1", "node_url": "https://n.example", "currency": "USD", "xno_per_unit": "1"},
		"remote http":  {"pay_to": payTo, "node_url": "http://n.example:7076", "currency": "USD", "xno_per_unit": "1"},
		"no currency":  {"pay_to": payTo, "node_url": "https://n.example", "currency": "", "xno_per_unit": "1"},
		"zero rate":    {"pay_to": payTo, "node_url": "https://n.example", "currency": "USD", "xno_per_unit": "0"},
		"float rate":   {"pay_to": payTo, "node_url": "https://n.example", "currency": "USD", "xno_per_unit": "1e3"},
		"no pay_to":    {"node_url": "https://n.example", "currency": "USD", "xno_per_unit": "1"},
	}
	for name, config := range bad {
		if _, err := registry.NormalizeConfig("Nano", config); err == nil {
			t.Errorf("%s: configuration accepted", name)
		}
		raw, _ := json.Marshal(config)
		if _, err := registry.Open(&payment.Payment{Platform: "Nano", Config: string(raw)}); err == nil {
			t.Errorf("%s: gateway opened", name)
		}
	}
}
