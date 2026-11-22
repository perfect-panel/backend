package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/callbacks"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/paypal"
)

const paypalTestConfig = `{"environment":"sandbox","client_id":"merchant","client_secret":"secret","webhook_id":"WH-1","locale":"zh-CN"}`
const approvalEvent = `{ "event_type": "CHECKOUT.ORDER.APPROVED", "resource": { "id": "PAY123" } }`
const captureEvent = `{ "event_type": "PAYMENT.CAPTURE.COMPLETED", "resource":{"id":"CAP123","supplementary_data":{"related_ids":{"order_id":"PAY123"}}} }`
const approvalURL = "https://www.sandbox.paypal.com/checkoutnow?token=PAY123"

type paypalPeer struct {
	t                                                    *testing.T
	status, captureStatus, fiat, amount, invoice, verify string
	creates, captures                                    int
	createKey, captureKey                                string
	onGet                                                func()
	onCapture                                            func()
	createFailure, verifyFailure, captureTimeout         bool
}

func (p *paypalPeer) order() string {
	payments := ""
	if p.captureStatus != "" {
		payments = fmt.Sprintf(`,"payments":{"captures":[{"id":"CAP123","status":%q,"amount":{"currency_code":%q,"value":%q}}]}`, p.captureStatus, p.fiat, p.amount)
	}
	return fmt.Sprintf(`{"id":"PAY123","intent":"CAPTURE","status":%q,"purchase_units":[{"reference_id":"order-1","invoice_id":%q,"amount":{"currency_code":%q,"value":%q}%s}],"links":[{"rel":"payer-action","href":%q}]}`, p.status, p.invoice, p.fiat, p.amount, payments, approvalURL)
}

func (p *paypalPeer) RoundTrip(r *http.Request) (*http.Response, error) {
	p.t.Helper()
	if r.URL.Scheme != "https" || r.URL.Host != "api-m.sandbox.paypal.com" {
		p.t.Fatalf("unexpected destination %s", r.URL)
	}
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/oauth2/token" {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "merchant" || secret != "secret" || r.FormValue("grant_type") != "client_credentials" {
			p.t.Fatal("invalid OAuth client credentials request")
		}
		_, _ = io.WriteString(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		return w.Result(), nil
	}
	if r.Header.Get("Authorization") != "Bearer access" {
		p.t.Fatal("missing merchant authorization")
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /v2/checkout/orders":
		p.creates++
		p.createKey = r.Header.Get("PayPal-Request-Id")
		p.checkCreate(r)
		if p.createFailure {
			return nil, errors.New("simulated lost create response")
		}
		// PayPal can respond with just HATEOAS links and an ID.
		_, _ = fmt.Fprintf(w, `{"id":"PAY123","status":"PAYER_ACTION_REQUIRED","links":[{"rel":"payer-action","href":%q}]}`, approvalURL)
	case "GET /v2/checkout/orders/PAY123":
		if p.onGet != nil {
			p.onGet()
		}
		if p.status == "MISSING" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"name":"RESOURCE_NOT_FOUND"}`)
		} else {
			_, _ = io.WriteString(w, p.order())
		}
	case "POST /v1/notifications/verify-webhook-signature":
		p.checkVerification(r)
		if p.verifyFailure {
			w.WriteHeader(503)
		} else {
			_, _ = fmt.Fprintf(w, `{"verification_status":%q}`, p.verify)
		}
	case "POST /v2/checkout/orders/PAY123/capture":
		p.captures++
		k := r.Header.Get("PayPal-Request-Id")
		if k == "" || k == p.createKey || (p.captureKey != "" && k != p.captureKey) {
			p.t.Fatal("unstable capture idempotency key")
		}
		p.captureKey = k
		if p.onCapture != nil {
			p.onCapture()
		}
		if p.captureStatus == "" {
			p.captureStatus = "COMPLETED"
		}
		p.status = "COMPLETED"
		if p.captureTimeout {
			return nil, errors.New("simulated lost capture response")
		}
		_, _ = io.WriteString(w, p.order())
	default:
		p.t.Fatalf("unexpected API request %s %s", r.Method, r.URL.Path)
	}
	return w.Result(), nil
}

func (p *paypalPeer) checkCreate(r *http.Request) {
	p.t.Helper()
	var v struct {
		Intent string                `json:"intent"`
		Units  []paypal.PurchaseUnit `json:"purchase_units"`
		Source struct {
			PayPal struct {
				Experience map[string]string `json:"experience_context"`
			} `json:"paypal"`
		} `json:"payment_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		p.t.Fatal(err)
	}
	if p.createKey == "" || v.Intent != "CAPTURE" || len(v.Units) != 1 || v.Units[0].Reference != "order-1" || v.Units[0].Invoice != "order-1" || !v.Units[0].Amount.Matches(1234, "USD") {
		p.t.Fatal("create did not bind merchant order and amount")
	}
	if v.Source.PayPal.Experience["locale"] != "zh-CN" || v.Source.PayPal.Experience["user_action"] != "PAY_NOW" || v.Source.PayPal.Experience["return_url"] != "https://www.example.test" {
		p.t.Fatal("missing checkout experience")
	}
}
func (p *paypalPeer) checkVerification(r *http.Request) {
	p.t.Helper()
	var v struct {
		paypal.Transmission
		ID    string          `json:"webhook_id"`
		Event json.RawMessage `json:"webhook_event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		p.t.Fatal(err)
	}
	if v.ID != "WH-1" || v.Transmission != deliveryHeaders() || (!bytes.Equal(v.Event, []byte(approvalEvent)) && !bytes.Equal(v.Event, []byte(captureEvent))) {
		p.t.Fatalf("verification fields or signed bytes changed: %s", v.Event)
	}
}
func deliveryHeaders() paypal.Transmission {
	return paypal.Transmission{ID: "delivery", Time: "2026-01-01T00:00:00Z", Signature: "signature", CertURL: "https://api.paypal.com/v1/notifications/certs/CERT", Algorithm: "SHA256withRSA"}
}

type paypalCase struct {
	*portalFixture
	peer         *paypalPeer
	method       *paymentEntity.Payment
	ctx          context.Context
	callback     *callbacks.Service
	closeService *checkout.Service
}

func newPayPalCase(t *testing.T) *paypalCase {
	p := &paypalPeer{t: t, status: "PAYER_ACTION_REQUIRED", fiat: "USD", amount: "12.34", invoice: "order-1", verify: "SUCCESS"}
	registry := gateway.NewRegistry(gateway.WithHTTPClient(&http.Client{Transport: p}))
	f := newPortalFixture(t, func(d *Deps) { d.Gateways = registry })
	f.currency = "USD"
	buyer, ctx := f.buyer(0, 0)
	method := f.h.Payment("PayPal", paypalTestConfig)
	f.pendingOrder("order-1", buyer.Id, 1234, method)
	return &paypalCase{portalFixture: f, peer: p, method: method, ctx: ctx,
		callback:     callbacks.NewService(f.h.Store.Order(), f.queue, registry),
		closeService: checkout.NewService(checkout.Deps{Orders: f.h.Store.Order(), Payments: f.h.Store.Payment(), Tx: f.h.Store, Queue: f.queue, Gateways: registry})}
}
func (f *paypalCase) start() {
	f.t.Helper()
	for range 2 {
		got, err := f.checkout(f.ctx, "order-1", "")
		if err != nil || got.Type != "url" || got.CheckoutUrl != approvalURL {
			f.t.Fatalf("checkout = %+v, %v", got, err)
		}
	}
	if f.peer.creates != 1 || f.h.ReloadOrder("order-1").TradeNo != "PAY123" {
		f.t.Fatal("duplicate create or unbound approval")
	}
}
func (f *paypalCase) notify(body string) error {
	return f.callback.Notify(context.WithValue(f.ctx, requestctx.CtxKeyPayment, f.method), gateway.Notification{HTTPMethod: http.MethodPost, Body: []byte(body), PayPalTransmission: deliveryHeaders()})
}

func TestPayPalApprovalCaptureAndRedelivery(t *testing.T) {
	f := newPayPalCase(t)
	f.start()
	f.peer.status = "APPROVED"
	f.peer.captureTimeout = true
	f.peer.onCapture = func() {
		if f.h.ReloadOrder("order-1").PaymentSession != order.PaymentSessionCapturing {
			t.Fatal("capture preceded the durable reservation")
		}
	}
	f.queue.ActivationErr = errors.New("queue unavailable")
	if err := f.notify(approvalEvent); err == nil {
		t.Fatal("queue failure acknowledged")
	}
	paid := f.h.ReloadOrder("order-1")
	if paid.Status != order.StatusPaid || paid.TradeNo != "PAY123" {
		t.Fatal("lost capture response should reconcile to the completed payment")
	}
	f.queue.ActivationErr = nil
	for _, event := range []string{captureEvent, approvalEvent, captureEvent} {
		if err := f.notify(event); err != nil {
			t.Fatal(err)
		}
	}
	if f.peer.captures != 1 || f.h.ReloadOrder("order-1").StateVersion != paid.StateVersion || len(f.queue.Activations) == 0 {
		t.Fatal("capture or settlement was repeated or activation was lost")
	}
}

func TestPayPalRefusesUnverifiedOrMismatchedPayments(t *testing.T) {
	for _, scenario := range []string{"invalid signature", "verification outage", "approval only", "wrong currency", "wrong amount", "wrong invoice", "pending capture", "refunded capture"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPayPalCase(t)
			f.start()
			f.peer.status = "APPROVED"
			event := approvalEvent
			switch scenario {
			case "invalid signature":
				f.peer.verify = "FAILURE"
			case "verification outage":
				f.peer.verifyFailure = true
			case "approval only":
				event = captureEvent
			case "wrong currency":
				f.peer.fiat = "EUR"
			case "wrong amount":
				f.peer.amount = "1.00"
			case "wrong invoice":
				f.peer.invoice = "other"
			case "pending capture":
				f.peer.status = "COMPLETED"
				f.peer.captureStatus = "PENDING"
				event = captureEvent
			case "refunded capture":
				event = captureEvent
				f.peer.status = "COMPLETED"
				f.peer.captureStatus = "REFUNDED"
			}
			err := f.notify(event)
			if err == nil || f.h.ReloadOrder("order-1").Status != order.StatusPending || f.peer.captures != 0 || len(f.queue.Activations) != 0 {
				t.Fatalf("unexpected settlement: %v", err)
			}
			if scenario == "verification outage" && errors.Is(err, gateway.ErrInvalidCallback) {
				t.Fatal("transient verification failure treated as invalid signature")
			}
		})
	}
}

func TestPayPalCancelSerializesWithCapture(t *testing.T) {
	f := newPayPalCase(t)
	f.start()
	closed, err := f.closeService.CloseByAdmin(f.ctx, "order-1", 1)
	if err != nil || !closed {
		t.Fatalf("close = %v, %v", closed, err)
	}
	f.peer.status = "APPROVED"
	if err := f.notify(approvalEvent); err != nil || f.peer.captures != 0 {
		t.Fatalf("approval after local cancellation: %v", err)
	}
	if reserved, err := f.h.Store.Order().ReservePaymentCapture(f.ctx, "order-1", "PAY123"); err != nil || reserved {
		t.Fatal("closed order obtained capture reservation")
	}
}
func TestPayPalReconciliationKeepsPendingCapture(t *testing.T) {
	f := newPayPalCase(t)
	f.start()
	f.peer.status = "APPROVED"
	f.peer.onCapture = func() { f.peer.captureStatus = "PENDING" }
	if _, err := f.closeService.CloseByAdmin(f.ctx, "order-1", 1); err == nil || f.h.ReloadOrder("order-1").Status != order.StatusPending {
		t.Fatal("pending capture was closed or settled")
	}
	f.peer.captureStatus = "COMPLETED"
	if closed, err := f.closeService.CloseByAdmin(f.ctx, "order-1", 1); err != nil || closed || f.h.ReloadOrder("order-1").Status != order.StatusPaid || f.peer.captures != 1 {
		t.Fatalf("reconciliation failed: %v", err)
	}
}
func TestPayPalAmbiguousCreateIsNotRepeated(t *testing.T) {
	f := newPayPalCase(t)
	f.peer.createFailure = true
	for range 2 {
		if _, err := f.checkout(f.ctx, "order-1", ""); err == nil {
			t.Fatal("returned an unbound checkout URL")
		}
	}
	if f.peer.creates != 1 || !strings.Contains(f.h.ReloadOrder("order-1").PaymentSession, "creating") {
		t.Fatal("unknown create lost its reservation")
	}
}

func TestPayPalCaptureReservationWinsAgainstStaleClose(t *testing.T) {
	f := newPayPalCase(t)
	f.start()
	f.peer.onGet = func() {
		reserved, err := f.h.Store.Order().ReservePaymentCapture(f.ctx, "order-1", "PAY123")
		if err != nil || !reserved {
			t.Fatalf("reserve: %v, %v", reserved, err)
		}
	}
	closed, err := f.closeService.CloseByAdmin(f.ctx, "order-1", 1)
	if err == nil || closed || f.h.ReloadOrder("order-1").Status != order.StatusPending {
		t.Fatal("stale unpaid order closed during capture")
	}
}

func TestPayPalExpiredOrderLookup(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		t.Run(fmt.Sprint("capture reserved=", reserved), func(t *testing.T) {
			f := newPayPalCase(t)
			f.start()
			if reserved {
				ok, err := f.h.Store.Order().ReservePaymentCapture(f.ctx, "order-1", "PAY123")
				if err != nil || !ok {
					t.Fatal("capture reservation failed")
				}
			}
			f.peer.status = "MISSING"
			closed, err := f.closeService.CloseByAdmin(f.ctx, "order-1", 1)
			if reserved && (closed || err == nil) {
				t.Fatal("missing resource cannot resolve a capture in flight")
			}
			if !reserved && (!closed || err != nil) {
				t.Fatalf("expired uncaptured order did not close: %v", err)
			}
		})
	}
}
