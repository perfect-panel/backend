package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing"
)

func TestPaymentNotifyHandler_writesErrorEnvelope_whenPlatformIsMissing(t *testing.T) {
	// Given
	engine := server.Default()
	engine.POST("/payment/notify", PaymentNotifyHandler(billing.New(billing.Deps{})))
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, http.StatusOK)
}

// Stripe reads the HTTP status: a missing payment method is a processing
// failure it must retry.
func TestPaymentNotifyHandler_preservesStripeRawPayloadAndSignature_whenPaymentIsMissing(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.Set("Stripe-Signature", "t=1,v1=test-signature")
	ctx.Request.SetBodyString(`{"id":"evt_test","type":"payment_intent.succeeded"}`)

	// When
	PaymentNotifyHandler(billing.New(billing.Deps{}))(context.WithValue(context.Background(), requestctx.CtxKeyPlatform, "Stripe"), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, http.StatusInternalServerError)
}

// An oversized Stripe payload cannot be authenticated; it is refused for
// good with 400.
func TestPaymentNotifyHandler_returnsExistingErrorEnvelope_whenStripePayloadExceedsHistoricalLimit(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.SetBody(bytes.Repeat([]byte("x"), 65_537))

	// When
	PaymentNotifyHandler(billing.New(billing.Deps{}))(context.WithValue(context.Background(), requestctx.CtxKeyPlatform, "Stripe"), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, http.StatusBadRequest)
}

// notifyService answers every callback of one style with a fixed outcome.
type notifyService struct {
	billing.Service
	style billing.PaymentCallbackStyle
	err   error
}

func (s notifyService) PaymentCallbackStyle(string) (billing.PaymentCallbackStyle, bool) {
	return s.style, true
}

func (s notifyService) PaymentNotify(context.Context, billing.PaymentNotification) error {
	return s.err
}

// serveNotify posts an EPay-shaped form callback of platform to the handler.
func serveNotify(t *testing.T, service billing.Service, platform string) *app.RequestContext {
	t.Helper()
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx.Request.SetBodyString("out_trade_no=order-1&trade_status=TRADE_SUCCESS&sign=test")
	PaymentNotifyHandler(service)(context.WithValue(context.Background(), requestctx.CtxKeyPlatform, platform), ctx)
	return ctx
}

// Stripe only reads the HTTP status of a webhook answer, so a failure must
// not be HTTP 200: a callback redelivery cannot fix is 400, a processing
// failure 500, and Stripe redelivers the latter. The other gateways keep the
// bodies their protocols demand.
func TestPaymentNotifyHandlerAnswersStripeFailuresWithAStatus(t *testing.T) {
	stripe := billing.PaymentCallbackStyle{Body: true, StatusFailure: true}
	invalid := fmt.Errorf("%w: verify signature", billing.ErrInvalidPaymentCallback)
	processing := errors.New("database unavailable")
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"cannot be authenticated", invalid, http.StatusBadRequest},
		{"processing failure", processing, http.StatusInternalServerError},
		{"accepted", nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run("Stripe "+tt.name, func(t *testing.T) {
			ctx := serveNotify(t, notifyService{style: stripe, err: tt.err}, "Stripe")
			if tt.err == nil {
				if ctx.Response.StatusCode() != http.StatusOK {
					t.Fatalf("status = %d, want 200", ctx.Response.StatusCode())
				}
				return
			}
			assertPaymentNotifyError(t, ctx, tt.status)
		})
	}
	epay := billing.PaymentCallbackStyle{UniqueParams: true, TextReply: true, TextFailure: true}
	if ctx := serveNotify(t, notifyService{style: epay, err: processing}, "EPay"); ctx.Response.StatusCode() != http.StatusBadRequest || string(ctx.Response.Body()) != "database unavailable" {
		t.Fatalf("EPay failure = %d %q, want 400 with the error text", ctx.Response.StatusCode(), ctx.Response.Body())
	}
	if ctx := serveNotify(t, notifyService{style: epay}, "EPay"); ctx.Response.StatusCode() != http.StatusOK || string(ctx.Response.Body()) != "success" {
		t.Fatalf("EPay success = %d %q, want 200 success", ctx.Response.StatusCode(), ctx.Response.Body())
	}
	alipay := billing.PaymentCallbackStyle{TextReply: true}
	assertPaymentNotifyError(t, serveNotify(t, notifyService{style: alipay, err: processing}, "AlipayF2F"), http.StatusOK)
}

func TestPaymentNotifyHandler_acknowledgesEPayFormFailure_whenPaymentIsMissing(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify?channel=web")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx.Request.SetBodyString("out_trade_no=order-1&trade_status=TRADE_SUCCESS&sign=test")

	// When
	PaymentNotifyHandler(billing.New(billing.Deps{}))(context.WithValue(context.Background(), requestctx.CtxKeyPlatform, "EPay"), ctx)

	// Then
	if got := ctx.Response.StatusCode(); got != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, got)
	}
	const acknowledgement = "payment config not found: ErrCode:500，ErrMsg:Internal Server Error"
	if got := string(ctx.Response.Body()); got != acknowledgement {
		t.Fatalf("expected form callback acknowledgement %q, got %q", acknowledgement, got)
	}
}

func TestPaymentNotifyHandlerRejectsRemovedCryptoSaaSPlatform(t *testing.T) {
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)

	PaymentNotifyHandler(billing.New(billing.Deps{}))(context.WithValue(context.Background(), requestctx.CtxKeyPlatform, "CryptoSaaS"), ctx)

	if got := ctx.Response.StatusCode(); got != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, got)
	}
	if got := string(ctx.Response.Body()); got != "unsupported payment platform" {
		t.Fatalf("unexpected response: %q", got)
	}
}

func TestNativeFormValues_prioritizesPostValue_whenQueryDuplicatesKey(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify?trade_status=query")
	ctx.Request.PostArgs().Add("trade_status", "body")

	// When
	values := nativeFormValues(ctx)

	// Then
	got := values["trade_status"]
	want := []string{"body", "query"}
	if len(got) != len(want) {
		t.Fatalf("expected %d values, got %d: %q", len(want), len(got), got)
	}
	for index, wantValue := range want {
		if got[index] != wantValue {
			t.Fatalf("expected value %d to be %q, got %q", index, wantValue, got[index])
		}
	}
}

func TestUniqueFormValuesRejectsDuplicateCallbackParameters(t *testing.T) {
	_, err := uniqueFormValues(map[string][]string{
		"out_trade_no": {"body-order", "query-order"},
	})
	if err == nil {
		t.Fatal("duplicate callback parameters must be rejected")
	}
}

func TestNotifyPayload_acceptsHistoricalLimitAndRejectsLargerPayload(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at historical limit", size: 65_536},
		{name: "over historical limit", size: 65_537, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			payload := bytes.Repeat([]byte("x"), test.size)

			// When
			got, err := notifyPayload(payload)

			// Then
			if (err != nil) != test.wantErr {
				t.Fatalf("expected error=%t, got %v", test.wantErr, err)
			}
			if !test.wantErr && !bytes.Equal(got, payload) {
				t.Fatal("expected payload to remain unchanged")
			}
		})
	}
}

// assertPaymentNotifyError checks a rejection answered with the error
// envelope: wantStatus and the generic message that hides the cause.
func assertPaymentNotifyError(t *testing.T, ctx *app.RequestContext, wantStatus int) {
	t.Helper()
	const wantMessage = "Internal Server Error"
	if got := ctx.Response.StatusCode(); got != wantStatus {
		t.Fatalf("expected status %d, got %d", wantStatus, got)
	}
	var response struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Msg != wantMessage {
		t.Fatalf("expected message %q, got %q", wantMessage, response.Msg)
	}
}

func TestGMPayAcknowledgement(t *testing.T) {
	style, ok := billing.New(billing.Deps{}).PaymentCallbackStyle("GMPay")
	if !ok || !style.Body {
		t.Fatal("GMPay requires the signed JSON body")
	}
	ctx := serveNotify(t, notifyService{style: style}, "GMPay")
	if ctx.Response.StatusCode() != http.StatusOK || string(ctx.Response.Body()) != "success" {
		t.Fatalf("acknowledgement = %d %q", ctx.Response.StatusCode(), ctx.Response.Body())
	}
	for _, tt := range []struct {
		err    error
		status int
	}{
		{billing.ErrInvalidPaymentCallback, http.StatusBadRequest},
		{errors.New("activation queue unavailable"), http.StatusInternalServerError},
	} {
		ctx := serveNotify(t, notifyService{style: style, err: tt.err}, "GMPay")
		assertPaymentNotifyError(t, ctx, tt.status)
	}
}

type paypalNotifyProbe struct {
	notifyService
	received billing.PaymentNotification
}

func (p *paypalNotifyProbe) PaymentNotify(_ context.Context, n billing.PaymentNotification) error {
	p.received = n
	return p.err
}
func TestPayPalWebhookDeliveryHeaders(t *testing.T) {
	style, ok := billing.New(billing.Deps{}).PaymentCallbackStyle("PayPal")
	if !ok || !style.StatusFailure {
		t.Fatal("missing PayPal status-based webhook")
	}
	probe := &paypalNotifyProbe{notifyService: notifyService{style: style}}
	ctx := server.Default().NewContext()
	ctx.Request.Header.SetMethod(http.MethodPost)
	for k, v := range map[string]string{"PAYPAL-TRANSMISSION-ID": "id", "PAYPAL-TRANSMISSION-TIME": "time", "PAYPAL-TRANSMISSION-SIG": "sig", "PAYPAL-CERT-URL": "cert", "PAYPAL-AUTH-ALGO": "algo"} {
		ctx.Request.Header.Set(k, v)
	}
	const body = `{ "event_type": "CHECKOUT.ORDER.APPROVED" }`
	ctx.Request.SetBodyString(body)
	PaymentNotifyHandler(probe)(context.WithValue(t.Context(), requestctx.CtxKeyPlatform, "PayPal"), ctx)
	h := probe.received.PayPalTransmission
	if h.ID != "id" || h.Time != "time" || h.Signature != "sig" || h.CertURL != "cert" || h.Algorithm != "algo" || string(probe.received.Body) != body {
		t.Fatal("lost signed delivery metadata")
	}
}
