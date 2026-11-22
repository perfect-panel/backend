package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/auth/deviceauth"
	appconfig "github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func TestAuthMiddleware_abortsWithTokenEnvelope_whenAuthorizationMissing(t *testing.T) {
	// Given
	engine := server.Default()
	downstreamRan := false
	engine.GET("/protected", AuthMiddleware(AuthDeps{}), func(_ context.Context, ctx *app.RequestContext) {
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
	queryData, queryTime, err := deviceauth.Encrypt([]byte(`{"page":2}`), secret)
	if err != nil {
		t.Fatalf("encrypt query: %v", err)
	}
	bodyData, bodyTime, err := deviceauth.Encrypt([]byte(`{"name":"device"}`), secret)
	if err != nil {
		t.Fatalf("encrypt body: %v", err)
	}
	requestBody, err := json.Marshal(map[string]string{"data": bodyData, "time": bodyTime, "sign": deviceauth.Sign(secret, "POST", "/device", "body", bodyData, bodyTime)})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	engine := server.Default()
	engine.POST("/device", DeviceMiddleware(func() appconfig.DeviceConfig {
		return appconfig.DeviceConfig{Enable: true, EnableSecurity: true, SecuritySecret: secret}
	}, deviceReplayClient(t)), func(requestCtx context.Context, ctx *app.RequestContext) {
		if loginType, _ := requestCtx.Value(requestctx.LoginType).(string); loginType != "device" {
			t.Errorf("expected derived request context login type %q, got %q", "device", loginType)
		}
		if secure, _ := requestCtx.Value(requestctx.CtxKeyDeviceSecure).(bool); !secure {
			t.Error("expected decrypted device request to carry the secure marker")
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

	values := url.Values{"data": {queryData}, "time": {queryTime}, "sign": {deviceauth.Sign(secret, "POST", "/device", "query", queryData, queryTime)}}
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
	plainText, err := deviceauth.Decrypt(response.Data.Data, secret, response.Data.Time)
	if err != nil {
		t.Fatalf("decrypt response: %v", err)
	}
	if plainText != `{"status":"ok"}` {
		t.Fatalf("expected encrypted response data %q, got %q", `{"status":"ok"}`, plainText)
	}
}

func TestDeviceMiddleware_rejectsPlaintextDeviceLoginRoute_withoutLoginTypeHeader(t *testing.T) {
	const secret = "device-secret"
	engine := server.Default()
	downstreamRan := false
	engine.POST("/v1/auth/login/device", DeviceMiddleware(func() appconfig.DeviceConfig {
		return appconfig.DeviceConfig{Enable: true, EnableSecurity: true, SecuritySecret: secret}
	}, deviceReplayClient(t)), func(_ context.Context, ctx *app.RequestContext) {
		downstreamRan = true
		ctx.String(http.StatusOK, "unreachable")
	})
	ctx := requestContext(engine, http.MethodPost, "/v1/auth/login/device")
	ctx.Request.SetBodyString(`{"identifier":"forged-device"}`)

	engine.ServeHTTP(context.Background(), ctx)

	if downstreamRan {
		t.Fatal("expected plaintext device login to abort before downstream handler")
	}
	assertErrorEnvelope(t, ctx.Response.Body(), xerr.InvalidCiphertext, "Invalid ciphertext")
}

func TestDeviceMiddleware_allowsUnrelatedPlaintextRoute_whenSecurityEnabled(t *testing.T) {
	engine := server.Default()
	engine.POST("/v1/auth/login", DeviceMiddleware(func() appconfig.DeviceConfig {
		return appconfig.DeviceConfig{Enable: true, EnableSecurity: true, SecuritySecret: "device-secret"}
	}, deviceReplayClient(t)), func(_ context.Context, ctx *app.RequestContext) {
		ctx.String(http.StatusOK, string(ctx.Request.Body()))
	})
	ctx := requestContext(engine, http.MethodPost, "/v1/auth/login")
	ctx.Request.SetBodyString(`{"email":"alice@example.com"}`)

	engine.ServeHTTP(context.Background(), ctx)

	if status := ctx.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, status)
	}
	if body := string(ctx.Response.Body()); body != `{"email":"alice@example.com"}` {
		t.Fatalf("unexpected response body %q", body)
	}
}

func TestDevicePayloadHelpers_roundTripRequestAndResponse_whenPayloadIsEncrypted(t *testing.T) {
	// Given
	const secret = "device-secret"
	ciphertext, iv, err := deviceauth.Encrypt([]byte(`{"name":"device"}`), secret)
	if err != nil {
		t.Fatalf("encrypt device request: %v", err)
	}
	requestBody, err := json.Marshal(map[string]string{"data": ciphertext, "time": iv, "sign": deviceauth.Sign(secret, "POST", "/device", "body", ciphertext, iv)})
	if err != nil {
		t.Fatalf("marshal encrypted request: %v", err)
	}
	requestCtx := app.NewContext(0)
	requestCtx.Request.Header.SetMethod("POST")
	requestCtx.Request.SetRequestURI("/device")
	requestCtx.Request.SetBody(requestBody)

	// When
	if err := DecryptDeviceRequest(context.Background(), requestCtx, secret, deviceReplayClient(t)); err != nil {
		t.Fatalf("expected encrypted request to decrypt: %v", err)
	}
	requestCtx.Response.SetBodyString(`{"data":{"status":"ok"}}`)
	if err := EncryptDeviceResponse(requestCtx, secret); err != nil {
		t.Fatalf("encrypt device response: %v", err)
	}

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
	plainText, err := deviceauth.Decrypt(response.Data.Data, secret, response.Data.Time)
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
	engine.GET("/v1/notify/:platform/:token", NotifyMiddleware(paymentMethods{payment: paymentConfig}), func(requestCtx context.Context, ctx *app.RequestContext) {
		platform, _ := requestCtx.Value(requestctx.CtxKeyPlatform).(string)
		configuredPayment, _ := requestCtx.Value(requestctx.CtxKeyPayment).(*payment.Payment)
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
	engine.GET("/v1/notify/:platform/:token", NotifyMiddleware(paymentMethods{payment: paymentConfig}), func(_ context.Context, ctx *app.RequestContext) {
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

// paymentMethods resolves the token of the one payment method it holds.
type paymentMethods struct {
	payment *payment.Payment
}

var _ PaymentMethods = paymentMethods{}

func (m paymentMethods) FindPaymentMethodByToken(_ context.Context, token string) (*payment.Payment, error) {
	if token != m.payment.Token {
		return nil, context.Canceled
	}
	return m.payment, nil
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
