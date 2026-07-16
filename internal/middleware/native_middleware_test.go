package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	pkgaes "github.com/perfect-panel/server/pkg/aes"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
)

func TestAuthMiddleware_abortsWithTokenEnvelope_whenAuthorizationMissing(t *testing.T) {
	// Given
	engine := server.Default()
	downstreamRan := false
	engine.GET("/protected", AuthMiddleware(&svc.ServiceContext{}), func(_ context.Context, ctx *app.RequestContext) {
		downstreamRan = true
		ctx.String(http.StatusOK, "unreachable")
	})

	ctx := requestContext(engine, http.MethodGet, "/protected")

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	if downstreamRan {
		t.Fatal("expected missing authorization to abort before the downstream handler")
	}
	assertErrorEnvelope(t, ctx.Response.Body(), xerr.ErrorTokenEmpty, "User token is empty")
}

func TestDeviceMiddleware_decryptsRequestAndEncryptsResponse_whenDeviceLogin(t *testing.T) {
	// Given
	const secret = "device-secret"
	queryData, queryTime, err := pkgaes.Encrypt([]byte(`{"page":2}`), secret)
	if err != nil {
		t.Fatalf("encrypt query: %v", err)
	}
	bodyData, bodyTime, err := pkgaes.Encrypt([]byte(`{"name":"device"}`), secret)
	if err != nil {
		t.Fatalf("encrypt body: %v", err)
	}
	requestBody, err := json.Marshal(map[string]string{"data": bodyData, "time": bodyTime})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	engine := server.Default()
	engine.POST("/device", DeviceMiddleware(&svc.ServiceContext{
		Config: appconfig.Config{Device: appconfig.DeviceConfig{Enable: true, SecuritySecret: secret}},
	}), func(requestCtx context.Context, ctx *app.RequestContext) {
		if loginType, _ := requestCtx.Value(constant.LoginType).(string); loginType != "device" {
			t.Errorf("expected derived request context login type %q, got %q", "device", loginType)
		}
		if got := ctx.Query("page"); got != "2" {
			t.Errorf("expected decrypted query page %q, got %q", "2", got)
		}
		if got := string(ctx.Request.Body()); got != `{"name":"device"}` {
			t.Errorf("expected decrypted request body, got %q", got)
		}
		ctx.Header("X-Device", "encrypted")
		ctx.JSON(http.StatusCreated, map[string]map[string]string{"data": {"status": "ok"}})
	})

	values := url.Values{"data": {queryData}, "time": {queryTime}}
	ctx := requestContext(engine, http.MethodPost, "/device?"+values.Encode())
	ctx.Request.Header.Set("Login-Type", "device")
	ctx.Request.SetBody(requestBody)

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	if status := ctx.Response.StatusCode(); status != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, status)
	}
	if got := string(ctx.Response.Header.Peek("X-Device")); got != "encrypted" {
		t.Fatalf("expected response header %q, got %q", "encrypted", got)
	}
	var response struct {
		Data struct {
			Data string `json:"data"`
			Time string `json:"time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal encrypted response: %v", err)
	}
	plainText, err := pkgaes.Decrypt(response.Data.Data, secret, response.Data.Time)
	if err != nil {
		t.Fatalf("decrypt response: %v", err)
	}
	if plainText != `{"status":"ok"}` {
		t.Fatalf("expected encrypted response data %q, got %q", `{"status":"ok"}`, plainText)
	}
}

func TestDevicePayloadHelpers_roundTripRequestAndResponse_whenPayloadIsEncrypted(t *testing.T) {
	// Given
	const secret = "device-secret"
	ciphertext, iv, err := pkgaes.Encrypt([]byte(`{"name":"device"}`), secret)
	if err != nil {
		t.Fatalf("encrypt device request: %v", err)
	}
	requestBody, err := json.Marshal(map[string]string{"data": ciphertext, "time": iv})
	if err != nil {
		t.Fatalf("marshal encrypted request: %v", err)
	}
	requestCtx := app.NewContext(0)
	requestCtx.Request.SetBody(requestBody)

	// When
	if !DecryptDeviceRequest(requestCtx, secret) {
		t.Fatal("expected encrypted request to decrypt")
	}
	requestCtx.Response.SetBodyString(`{"data":{"status":"ok"}}`)
	EncryptDeviceResponse(requestCtx, secret)

	// Then
	if body := string(requestCtx.Request.Body()); body != `{"name":"device"}` {
		t.Fatalf("expected decrypted request body, got %q", body)
	}
	var response struct {
		Data struct {
			Data string `json:"data"`
			Time string `json:"time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(requestCtx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal encrypted response: %v", err)
	}
	plainText, err := pkgaes.Decrypt(response.Data.Data, secret, response.Data.Time)
	if err != nil {
		t.Fatalf("decrypt response data: %v", err)
	}
	if plainText != `{"status":"ok"}` {
		t.Fatalf("expected encrypted response data %q, got %q", `{"status":"ok"}`, plainText)
	}
}

func TestNotifyMiddleware_propagatesPaymentContext_whenTokenResolves(t *testing.T) {
	// Given
	paymentConfig := &payment.Payment{Platform: "stripe", Token: "notify-token"}
	engine := server.Default()
	engine.GET("/v1/notify/:platform/:token", NotifyMiddleware(&svc.ServiceContext{
		Store: paymentStore{payment: paymentRepository{payment: paymentConfig}},
	}), func(requestCtx context.Context, ctx *app.RequestContext) {
		platform, _ := requestCtx.Value(constant.CtxKeyPlatform).(string)
		configuredPayment, _ := requestCtx.Value(constant.CtxKeyPayment).(*payment.Payment)
		if platform != paymentConfig.Platform || configuredPayment != paymentConfig {
			ctx.String(http.StatusInternalServerError, "payment context missing")
			return
		}
		ctx.String(http.StatusOK, "payment context propagated")
	})
	ctx := requestContext(engine, http.MethodGet, "/v1/notify/stripe/notify-token")

	// When
	engine.ServeHTTP(context.Background(), ctx)

	// Then
	if status := ctx.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, status)
	}
	if body := string(ctx.Response.Body()); body != "payment context propagated" {
		t.Fatalf("expected payment context response, got %q", body)
	}
}

func TestNotifyMiddlewareRejectsRoutePlatformThatDoesNotMatchToken(t *testing.T) {
	paymentConfig := &payment.Payment{Platform: "EPay", Token: "notify-token"}
	engine := server.Default()
	downstreamRan := false
	engine.GET("/v1/notify/:platform/:token", NotifyMiddleware(&svc.ServiceContext{
		Store: paymentStore{payment: paymentRepository{payment: paymentConfig}},
	}), func(_ context.Context, ctx *app.RequestContext) {
		downstreamRan = true
		ctx.String(http.StatusOK, "unreachable")
	})
	ctx := requestContext(engine, http.MethodGet, "/v1/notify/Stripe/notify-token")

	engine.ServeHTTP(context.Background(), ctx)

	if downstreamRan {
		t.Fatal("platform mismatch must abort before callback handling")
	}
	if status := ctx.Response.StatusCode(); status != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, status)
	}
}

type paymentStore struct {
	repository.Store
	payment repository.PaymentRepo
}

func (s paymentStore) Payment() repository.PaymentRepo {
	return s.payment
}

type paymentRepository struct {
	repository.PaymentRepo
	payment *payment.Payment
}

func (r paymentRepository) FindOneByPaymentToken(_ context.Context, token string) (*payment.Payment, error) {
	if token != r.payment.Token {
		return nil, context.Canceled
	}
	return r.payment, nil
}

func requestContext(engine *server.Hertz, method string, uri string) *app.RequestContext {
	ctx := engine.NewContext()
	ctx.Request.SetRequestURI(uri)
	ctx.Request.Header.SetMethod(method)
	return ctx
}

func assertErrorEnvelope(t *testing.T, body []byte, wantCode uint32, wantMessage string) {
	t.Helper()
	var response struct {
		Code uint32 `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if response.Code != wantCode || response.Msg != wantMessage {
		t.Fatalf("expected error envelope (%d, %q), got (%d, %q)", wantCode, wantMessage, response.Code, response.Msg)
	}
}
