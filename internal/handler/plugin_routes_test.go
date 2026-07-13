package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	pkgaes "github.com/perfect-panel/server/pkg/aes"
	"github.com/perfect-panel/server/pkg/xerr"
)

func TestNormalizePluginDispatchPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "/"},
		{name: "wildcard", in: "*", want: "/"},
		{name: "with slash", in: "/webhook", want: "/webhook"},
		{name: "without slash", in: "webhook", want: "/webhook"},
		{name: "nested without slash", in: "api/webhook", want: "/api/webhook"},
		{name: "trim trailing slash", in: "/webhook/", want: "/webhook"},
		{name: "trim spaces", in: " webhook ", want: "/webhook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePluginDispatchPath(tt.in); got != tt.want {
				t.Fatalf("normalizePluginDispatchPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPluginDispatcher_returnsNotFound_whenRouteIsMissing(t *testing.T) {
	// Given
	manager := plugin.NewManager(&plugin.HostEnv{Config: appconfig.Config{}})
	engine := server.Default()
	RegisterPluginHandlers(engine, &svc.ServiceContext{}, manager)

	for _, method := range []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodOptions,
		http.MethodHead,
	} {
		t.Run(method, func(t *testing.T) {
			requestCtx := pluginRequestContext(engine, method, "/v1/plugin/demo/missing")

			// When
			engine.ServeHTTP(context.Background(), requestCtx)

			// Then
			if status := requestCtx.Response.StatusCode(); status != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, status)
			}
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(requestCtx.Response.Body(), &response); err != nil {
				t.Fatalf("unmarshal not-found response: %v", err)
			}
			want := "plugin route not found: " + method + " demo/missing"
			if response.Error != want {
				t.Fatalf("unexpected not-found error %q, want %q", response.Error, want)
			}
		})
	}
}

func TestBuildPluginHandleRequest_replaysRawBody_whenBuildingPluginRequest(t *testing.T) {
	// Given
	requestCtx := app.NewContext(0)
	requestCtx.Request.Header.SetMethod(http.MethodPost)
	requestCtx.Request.SetRequestURI("/v1/plugin/demo/echo?tag=one&tag=two")
	requestCtx.Request.Header.Set("X-Input", "present")
	requestCtx.Request.SetBodyString("raw-body")

	// When
	request := buildPluginHandleRequest(context.Background(), requestCtx)

	// Then
	if body := string(request.Body); body != "raw-body" {
		t.Fatalf("expected plugin request body %q, got %q", "raw-body", body)
	}
	if body := string(requestCtx.Request.Body()); body != "raw-body" {
		t.Fatalf("expected Hertz request body replay %q, got %q", "raw-body", body)
	}
	if got := request.Query["tag"].Values; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected query values %#v", got)
	}
	if got := request.Headers["X-Input"].Values; len(got) != 1 || got[0] != "present" {
		t.Fatalf("unexpected request header values %#v", got)
	}
}

func TestApplyWASMMiddlewareResponse_modifiesPluginRequestHeaders_whenActionIsModify(t *testing.T) {
	// Given
	requestCtx := app.NewContext(0)
	requestCtx.Request.Header.Set("X-Original", "unchanged")

	// When
	proceed := applyWASMMiddlewareResponse(requestCtx, &pluginv1.MiddlewareResponse{
		Action:  "modify",
		Headers: map[string]string{"X-Added": "yes"},
	})

	// Then
	if !proceed {
		t.Fatal("expected modifying middleware to continue")
	}
	if got := string(requestCtx.Request.Header.Peek("X-Added")); got != "yes" {
		t.Fatalf("expected modified request header %q, got %q", "yes", got)
	}
	if got := string(requestCtx.Response.Header.Peek("X-Added")); got != "" {
		t.Fatalf("expected no response header for modify action, got %q", got)
	}
}

func TestApplyWASMMiddlewareResponse_abortsWithPluginResponse_whenActionIsAbort(t *testing.T) {
	// Given
	requestCtx := app.NewContext(0)

	// When
	proceed := applyWASMMiddlewareResponse(requestCtx, &pluginv1.MiddlewareResponse{
		Action:  "abort",
		Status:  http.StatusForbidden,
		Body:    []byte("denied"),
		Headers: map[string]string{"X-Reason": "blocked"},
	})

	// Then
	if proceed {
		t.Fatal("expected abort middleware to stop dispatch")
	}
	if status := requestCtx.Response.StatusCode(); status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if header := string(requestCtx.Response.Header.Peek("X-Reason")); header != "blocked" {
		t.Fatalf("expected response header %q, got %q", "blocked", header)
	}
	if body := string(requestCtx.Response.Body()); body != "denied" {
		t.Fatalf("expected response body %q, got %q", "denied", body)
	}
	if !requestCtx.IsAborted() {
		t.Fatal("expected middleware abort to abort Hertz context")
	}
}

func TestWritePluginResponse_propagatesHeadersStatusAndBody(t *testing.T) {
	// Given
	responseCtx := app.NewContext(0)

	// When
	writePluginResponse(responseCtx, &pluginv1.HandleResponse{
		Status:  http.StatusCreated,
		Body:    []byte("payload"),
		Headers: map[string]string{"X-Plugin": "ok"},
	})

	// Then
	if status := responseCtx.Response.StatusCode(); status != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, status)
	}
	if header := string(responseCtx.Response.Header.Peek("X-Plugin")); header != "ok" {
		t.Fatalf("expected response header %q, got %q", "ok", header)
	}
	if body := string(responseCtx.Response.Body()); body != "payload" {
		t.Fatalf("expected response body %q, got %q", "payload", body)
	}
}

func TestApplyAuthMiddleware_abortsWithTokenEnvelope_whenAuthorizationIsMissing(t *testing.T) {
	// Given
	requestCtx := app.NewContext(0)
	requestCtx.Request.SetRequestURI("/v1/plugin/demo/protected")

	// When
	_, proceed := applyAuthMiddleware(context.Background(), requestCtx, &svc.ServiceContext{})

	// Then
	if proceed {
		t.Fatal("expected missing authorization to stop dispatch")
	}
	if !requestCtx.IsAborted() {
		t.Fatal("expected missing authorization to abort Hertz context")
	}
	var response struct {
		Code uint32 `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(requestCtx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal auth error response: %v", err)
	}
	if response.Code != xerr.ErrorTokenEmpty || response.Msg != "User token is empty" {
		t.Fatalf("unexpected auth error response (%d, %q)", response.Code, response.Msg)
	}
}

func TestApplyDeviceMiddleware_finalizesEncryptedResponse_whenDeviceLogin(t *testing.T) {
	// Given
	const secret = "device-secret"
	ciphertext, iv, err := pkgaes.Encrypt([]byte(`{"name":"device"}`), secret)
	if err != nil {
		t.Fatalf("encrypt device request: %v", err)
	}
	requestCtx := app.NewContext(0)
	requestCtx.Request.Header.Set("Login-Type", "device")
	requestCtx.Request.SetBodyString(`{"data":"` + ciphertext + `","time":"` + iv + `"}`)
	svcCtx := &svc.ServiceContext{Config: appconfig.Config{Device: appconfig.DeviceConfig{Enable: true, SecuritySecret: secret}}}

	// When
	_, finalize, proceed := applyDeviceMiddleware(context.Background(), requestCtx, svcCtx)
	if !proceed {
		t.Fatal("expected valid device request to continue")
	}
	if body := string(requestCtx.Request.Body()); body != `{"name":"device"}` {
		t.Fatalf("expected decrypted request body, got %q", body)
	}
	writePluginResponse(requestCtx, &pluginv1.HandleResponse{Status: http.StatusCreated, Body: []byte(`{"data":{"status":"ok"}}`)})
	finalize()

	// Then
	if status := requestCtx.Response.StatusCode(); status != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, status)
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
		t.Fatalf("decrypt device response: %v", err)
	}
	if plainText != `{"status":"ok"}` {
		t.Fatalf("expected encrypted response data %q, got %q", `{"status":"ok"}`, plainText)
	}
	if !requestCtx.IsAborted() {
		t.Fatal("expected device finalization to abort Hertz context")
	}
}

func pluginRequestContext(engine *server.Hertz, method string, uri string) *app.RequestContext {
	requestCtx := engine.NewContext()
	requestCtx.Request.SetRequestURI(uri)
	requestCtx.Request.Header.SetMethod(method)
	return requestCtx
}
