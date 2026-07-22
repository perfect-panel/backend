package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
)

func TestPaymentNotifyHandler_writesErrorEnvelope_whenPlatformIsMissing(t *testing.T) {
	// Given
	engine := server.Default()
	engine.POST("/payment/notify", PaymentNotifyHandler(&svc.ServiceContext{}))
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, "Internal Server Error")
}

func TestPaymentNotifyHandler_preservesStripeRawPayloadAndSignature_whenPaymentIsMissing(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.Header.Set("Stripe-Signature", "t=1,v1=test-signature")
	ctx.Request.SetBodyString(`{"id":"evt_test","type":"payment_intent.succeeded"}`)

	// When
	PaymentNotifyHandler(&svc.ServiceContext{})(context.WithValue(context.Background(), constant.CtxKeyPlatform, "Stripe"), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, "Internal Server Error")
}

func TestPaymentNotifyHandler_returnsExistingErrorEnvelope_whenStripePayloadExceedsHistoricalLimit(t *testing.T) {
	// Given
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI("/payment/notify")
	ctx.Request.Header.SetMethod(http.MethodPost)
	ctx.Request.SetBody(bytes.Repeat([]byte("x"), 65_537))

	// When
	PaymentNotifyHandler(&svc.ServiceContext{})(context.WithValue(context.Background(), constant.CtxKeyPlatform, "Stripe"), ctx)

	// Then
	assertPaymentNotifyError(t, ctx, "Internal Server Error")
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
	PaymentNotifyHandler(&svc.ServiceContext{})(context.WithValue(context.Background(), constant.CtxKeyPlatform, "EPay"), ctx)

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

	PaymentNotifyHandler(&svc.ServiceContext{})(context.WithValue(context.Background(), constant.CtxKeyPlatform, "CryptoSaaS"), ctx)

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

func TestStripePayload_acceptsHistoricalLimitAndRejectsLargerPayload(t *testing.T) {
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
			got, err := stripePayload(payload)

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

func assertPaymentNotifyError(t *testing.T, ctx *app.RequestContext, wantMessage string) {
	t.Helper()
	if got := ctx.Response.StatusCode(); got != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, got)
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
