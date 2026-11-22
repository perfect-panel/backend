package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	appconfig "github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// envelopeOf serves a request against the registered routes and returns the
// code and message of its envelope.
func envelopeOf(t *testing.T, config appconfig.Config, path string, headers map[string]string) (uint32, string) {
	t.Helper()
	router := server.Default()
	RegisterHandlers(router, Dependencies{Config: config})
	ctx := router.NewContext()
	ctx.Request.SetRequestURI(path)
	ctx.Request.Header.SetMethod(http.MethodPost)
	for key, value := range headers {
		ctx.Request.Header.Set(key, value)
	}
	ctx.Request.Header.SetContentTypeBytes([]byte("application/json"))
	ctx.Request.SetBodyString(`{}`)
	router.ServeHTTP(context.Background(), ctx)
	var response struct {
		Code uint32 `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("unmarshal %s envelope %q: %v", path, ctx.Response.Body(), err)
	}
	return response.Code, response.Msg
}

// deviceSecurity is the device transport switched on without a secret, so a
// request the transport handles is answered with SecretIsEmpty: the answer
// shows which middleware ran first.
var deviceSecurity = appconfig.Config{Runtime: appconfig.Runtime{Device: appconfig.DeviceConfig{Enable: true, EnableSecurity: true}}}

// On the verification-code routes the optional authentication runs before
// the device transport, as on /v1/public: a request that declares the device
// transport but carries a bad session is refused for the session, before the
// transport looks at it. The other /v1/common routes keep the transport
// alone.
func TestVerificationCodeRoutesAuthenticateBeforeTheDeviceTransport(t *testing.T) {
	logtest.Discard(t)
	headers := map[string]string{"Login-Type": "device", "Authorization": "not-a-session"}
	for _, path := range []string{"/v1/common/send_code", "/v1/common/send_sms_code"} {
		// Without a session store the session is refused as unavailable;
		// either way it is the session, not the transport, that answers.
		code, msg := envelopeOf(t, deviceSecurity, path, headers)
		if code != xerr.InvalidAccess && code != xerr.ErrorTokenExpire {
			t.Fatalf("%s: envelope = (%d, %q), want the session refused before the device transport", path, code, msg)
		}
		// Without a session the transport is reached.
		code, msg = envelopeOf(t, deviceSecurity, path, map[string]string{"Login-Type": "device"})
		if code != xerr.SecretIsEmpty {
			t.Fatalf("%s: envelope = (%d, %q), want the device transport (%d)", path, code, msg, xerr.SecretIsEmpty)
		}
	}
	code, msg := envelopeOf(t, deviceSecurity, "/v1/common/check_verification_code", headers)
	if code != xerr.SecretIsEmpty {
		t.Fatalf("/v1/common/check_verification_code: envelope = (%d, %q), want the device transport alone (%d)", code, msg, xerr.SecretIsEmpty)
	}
}

// The OAuth sign-in routes carry the device transport like the other
// sign-in routes: a device declaring the transport gets the envelope, a
// browser without the header reaches the handler.
func TestOAuthRoutesCarryTheDeviceTransport(t *testing.T) {
	logtest.Discard(t)
	for _, path := range []string{"/v1/auth/oauth/login", "/v1/auth/oauth/login/token", "/v1/auth/oauth/callback/apple"} {
		code, msg := envelopeOf(t, deviceSecurity, path, map[string]string{"Login-Type": "device"})
		if code != xerr.SecretIsEmpty {
			t.Fatalf("%s with the device transport: envelope = (%d, %q), want %d", path, code, msg, xerr.SecretIsEmpty)
		}
	}
	// A browser's request is untouched: it reaches the handler, whose
	// validation refuses the empty body.
	code, msg := envelopeOf(t, deviceSecurity, "/v1/auth/oauth/login", nil)
	if code != xerr.InvalidParams {
		t.Fatalf("/v1/auth/oauth/login from a browser: envelope = (%d, %q), want the handler's validation (%d)", code, msg, xerr.InvalidParams)
	}
}

// A restart is a mutation and takes a POST only.
func TestRestartIsAPost(t *testing.T) {
	router := server.New()
	RegisterHandlers(router, Dependencies{})
	methods := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Path == "/v1/admin/tool/restart" {
			methods[route.Method] = true
		}
	}
	if !methods[http.MethodPost] || methods[http.MethodGet] {
		t.Fatalf("restart methods = %v, want POST only", methods)
	}
}
