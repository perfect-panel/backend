package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The production routes of the handlers.
const (
	appleCallbackPath = "/v1/auth/oauth/callback/apple"
	loginPath         = "/v1/auth/oauth/login"
	tokenPath         = "/v1/auth/oauth/login/token"
)

const (
	formPost = "application/x-www-form-urlencoded"
	jsonBody = "application/json"
)

// requestMetadata is what the access-log middleware resolved for the
// request: the flows read the client address and user agent from it.
var requestMetadata = requestmeta.New("203.0.113.9", "oauth-client/2.0")

// oauthFacade is what the three handlers need together; the recording fake
// and the real identity facade both provide it.
type oauthFacade interface {
	AppleLoginCallbackService
	OAuthLoginService
	OAuthLoginGetTokenService
}

// recordingFacade records what the handlers hand to the identity facade and
// answers with what the test set.
type recordingFacade struct {
	calls    int
	metadata requestmeta.Metadata
	apple    dto.AppleLoginCallbackRequest
	login    dto.OAuthLoginRequest
	token    dto.OAuthLoginGetTokenRequest
	redirect identity.AppleLoginRedirect
	err      error
}

var _ oauthFacade = (*recordingFacade)(nil)

// record counts a call and notes the request metadata of its context, and
// returns the error the test set.
func (f *recordingFacade) record(ctx context.Context) error {
	f.calls++
	f.metadata, _ = requestmeta.From(ctx)
	return f.err
}

func (f *recordingFacade) AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*identity.AppleLoginRedirect, error) {
	f.apple = *req
	if err := f.record(ctx); err != nil {
		return nil, err
	}
	redirect := f.redirect
	return &redirect, nil
}

func (f *recordingFacade) OAuthLogin(ctx context.Context, req *dto.OAuthLoginRequest) (*dto.OAuthLoginResponse, error) {
	f.login = *req
	if err := f.record(ctx); err != nil {
		return nil, err
	}
	return &dto.OAuthLoginResponse{Redirect: "https://provider.example/authorize?state=state-1"}, nil
}

func (f *recordingFacade) OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (*dto.LoginResponse, error) {
	f.token = *req
	if err := f.record(ctx); err != nil {
		return nil, err
	}
	return &dto.LoginResponse{Token: "session-token"}, nil
}

// newRouter serves the handlers on their production routes behind a stand-in
// for the access-log middleware, which puts requestMetadata in the context.
func newRouter(t *testing.T, facade oauthFacade) *route.Engine {
	t.Helper()
	logtest.Discard(t)
	h := server.New()
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		c.Next(requestmeta.With(ctx, requestMetadata))
	})
	h.POST(appleCallbackPath, AppleLoginCallbackHandler(facade))
	h.POST(loginPath, OAuthLoginHandler(facade))
	h.POST(tokenPath, OAuthLoginGetTokenHandler(facade))
	return h.Engine
}

// post sends body to target with the content type.
func post(engine *route.Engine, target, contentType, body string) *ut.ResponseRecorder {
	return ut.PerformRequest(engine, http.MethodPost, target,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: contentType})
}

// envelope is the JSON body httpx.HttpResult and httpx.ParamErrorResult write.
type envelope struct {
	Code uint32          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// decodeEnvelope reads the envelope, which always travels with HTTP 200 and
// never with a redirect.
func decodeEnvelope(t *testing.T, w *ut.ResponseRecorder) envelope {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body)
	}
	if location := w.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q beside the envelope", location)
	}
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	return e
}

// assertFailure checks the envelope reports code with msg and no data.
func assertFailure(t *testing.T, w *ut.ResponseRecorder, code uint32, msg string) {
	t.Helper()
	e := decodeEnvelope(t, w)
	if e.Code != code || e.Msg != msg || e.Data != nil {
		t.Fatalf("envelope = {%d %q %s}, want {%d %q} without data", e.Code, e.Msg, e.Data, code, msg)
	}
}

// assertSuccess checks the envelope reports success and decodes its data.
func assertSuccess(t *testing.T, w *ut.ResponseRecorder, data any) {
	t.Helper()
	e := decodeEnvelope(t, w)
	if e.Code != xerr.SUCCESS || e.Msg != "success" {
		t.Fatalf("envelope = {%d %q %s}, want success", e.Code, e.Msg, e.Data)
	}
	if err := json.Unmarshal(e.Data, data); err != nil {
		t.Fatalf("data %s: %v", e.Data, err)
	}
}

// Apple posts its callback as a form. The handler hands the code, identity
// token and state to the facade; a field in the form wins over the same one
// in the query string, from which a callback without a body is read.
func TestAppleCallbackHandsTheFormPostToTheFacade(t *testing.T) {
	for name, tc := range map[string]struct {
		query, body string
		want        dto.AppleLoginCallbackRequest
	}{
		"form post": {"", "code=apple-code&id_token=apple-id-token&state=state-1",
			dto.AppleLoginCallbackRequest{Code: "apple-code", IDToken: "apple-id-token", State: "state-1"}},
		"form over query": {"?code=query-code&state=query-state", "code=apple-code&state=state-1",
			dto.AppleLoginCallbackRequest{Code: "apple-code", State: "state-1"}},
		"query only": {"?code=query-code&state=query-state", "",
			dto.AppleLoginCallbackRequest{Code: "query-code", State: "query-state"}},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingFacade{redirect: identity.AppleLoginRedirect{StatusCode: http.StatusFound, Location: "https://panel.example/oauth"}}
			post(newRouter(t, facade), appleCallbackPath+tc.query, formPost, tc.body)
			if facade.calls != 1 || facade.apple != tc.want {
				t.Fatalf("calls = %d, request = %+v; want one call with %+v", facade.calls, facade.apple, tc.want)
			}
		})
	}
}

// The browser follows the redirect the facade chose with the facade's status
// code: 302 to the page the state was issued for, 307 back to the site host.
// The answer carries nothing else, neither a body nor a cookie: the session
// is only issued by the token exchange the redirected page makes.
func TestAppleCallbackRedirectsWhereTheFacadeSays(t *testing.T) {
	for _, redirect := range []identity.AppleLoginRedirect{
		{StatusCode: http.StatusFound, Location: "https://panel.example/oauth/apple?code=apple-code&method=apple&state=state-1"},
		{StatusCode: http.StatusTemporaryRedirect, Location: "https://panel.example"},
	} {
		t.Run(http.StatusText(redirect.StatusCode), func(t *testing.T) {
			w := post(newRouter(t, &recordingFacade{redirect: redirect}), appleCallbackPath, formPost, "code=apple-code&state=state-1")
			if w.Code != redirect.StatusCode || w.Header().Get("Location") != redirect.Location {
				t.Fatalf("answer = %d to %q, want %d to %q", w.Code, w.Header().Get("Location"), redirect.StatusCode, redirect.Location)
			}
			if w.Body.Len() != 0 || len(w.Header().Peek("Set-Cookie")) != 0 {
				t.Fatalf("redirect carries body %q and cookie %q", w.Body, w.Header().Peek("Set-Cookie"))
			}
		})
	}
}

// A body that does not parse is answered with 400 and the handler's own error
// object, not the envelope the other endpoints use, and never reaches the
// facade. Only a JSON body can fail so: a form is read leniently.
func TestAppleCallbackRejectsAnUnparsableBody(t *testing.T) {
	facade := &recordingFacade{}
	w := post(newRouter(t, facade), appleCallbackPath, jsonBody, `{"code":`)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"Invalid request data"}` {
		t.Fatalf("answer = %d %q, want 400 with the error object", w.Code, w.Body)
	}
	if got := string(w.Header().ContentType()); got != "application/json; charset=utf-8" || w.Header().Get("Location") != "" {
		t.Fatalf("content type %q, Location %q", got, w.Header().Get("Location"))
	}
	if facade.calls != 0 {
		t.Fatalf("facade called %d times for an unparsable body", facade.calls)
	}
}

// When the facade fails, the browser gets the error envelope instead of a
// redirect: the code the facade chose, or the generic one for an error
// without a code.
func TestAppleCallbackReportsAFacadeFailureInTheEnvelope(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code uint32
		msg  string
	}{
		"coded":   {xerr.Errorf(xerr.OAuthStateInvalid, "peek apple state"), xerr.OAuthStateInvalid, "OAuth state is invalid or expired"},
		"uncoded": {errors.New("redis unavailable"), xerr.ERROR, "Internal Server Error"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(newRouter(t, &recordingFacade{err: tc.err}), appleCallbackPath, formPost, "code=apple-code&state=state-1")
			assertFailure(t, w, tc.code, tc.msg)
		})
	}
}

// The login hands the method and redirect to the facade and answers with the
// authorization URL the frontend sends the browser to.
func TestOAuthLoginAnswersWithTheAuthorizationURL(t *testing.T) {
	facade := &recordingFacade{}
	w := post(newRouter(t, facade), loginPath, jsonBody, `{"method":"github","redirect":"https://panel.example/oauth/github"}`)

	var data dto.OAuthLoginResponse
	assertSuccess(t, w, &data)
	if data.Redirect != "https://provider.example/authorize?state=state-1" {
		t.Fatalf("redirect = %q", data.Redirect)
	}
	if want := (dto.OAuthLoginRequest{Method: "github", Redirect: "https://panel.example/oauth/github"}); facade.calls != 1 || facade.login != want {
		t.Fatalf("calls = %d, request = %+v; want one call with %+v", facade.calls, facade.login, want)
	}
}

// The provider callback travels as a JSON object: the facade receives the
// decoded map it reads the code and state (or Telegram's signed result)
// from, with the invite code, the Turnstile token and the request metadata
// that registration's IP limit and the login audit use.
func TestOAuthLoginGetTokenHandsTheCallbackObjectToTheFacade(t *testing.T) {
	facade := &recordingFacade{}
	w := post(newRouter(t, facade), tokenPath, jsonBody,
		`{"method":"github","callback":{"code":"github-code","state":"state-1"},"invite":"INVITE-1","cf_token":"turnstile-1"}`)

	var data dto.LoginResponse
	assertSuccess(t, w, &data)
	if data.Token != "session-token" {
		t.Fatalf("token = %q", data.Token)
	}
	want := dto.OAuthLoginGetTokenRequest{
		Method:   "github",
		Callback: map[string]any{"code": "github-code", "state": "state-1"},
		Invite:   "INVITE-1",
		CfToken:  "turnstile-1",
	}
	if facade.calls != 1 || !reflect.DeepEqual(facade.token, want) {
		t.Fatalf("calls = %d, request = %#v; want one call with %#v", facade.calls, facade.token, want)
	}
	if facade.metadata != requestMetadata {
		t.Fatalf("metadata = %+v, want the request's %+v", facade.metadata, requestMetadata)
	}
}

// A JSON request that does not parse or misses a required field is a
// parameter error and never reaches the facade. The validator names the
// missing field; a decoding error's text is the JSON decoder's own.
func TestOAuthLoginHandlersRejectInvalidRequestsBeforeTheFacade(t *testing.T) {
	for name, tc := range map[string]struct {
		path, body, msg string
	}{
		"login without method":   {loginPath, `{"redirect":"https://panel.example/oauth"}`, "Method is a required field"},
		"login unparsable":       {loginPath, `{"method":`, ""},
		"token without callback": {tokenPath, `{"method":"github"}`, "Callback is a required field"},
		"token null callback":    {tokenPath, `{"method":"github","callback":null}`, "Callback is a required field"},
		"token without method":   {tokenPath, `{"callback":{"code":"c","state":"s"}}`, "Method is a required field"},
		"token unparsable":       {tokenPath, `{"method":"github","callback":{`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			facade := &recordingFacade{}
			e := decodeEnvelope(t, post(newRouter(t, facade), tc.path, jsonBody, tc.body))
			if e.Code != xerr.InvalidParams || (tc.msg != "" && e.Msg != tc.msg) || e.Data != nil {
				t.Fatalf("envelope = {%d %q %s}, want code %d with %q", e.Code, e.Msg, e.Data, xerr.InvalidParams, tc.msg)
			}
			if facade.calls != 0 {
				t.Fatalf("facade called %d times for an invalid request", facade.calls)
			}
		})
	}
}

// The facade's error reaches the client as its code and message.
func TestOAuthLoginHandlersReportFacadeErrorsInTheEnvelope(t *testing.T) {
	for path, body := range map[string]string{
		loginPath: `{"method":"github","redirect":"https://panel.example/oauth"}`,
		tokenPath: `{"method":"github","callback":{"code":"c","state":"s"}}`,
	} {
		t.Run(path, func(t *testing.T) {
			facade := &recordingFacade{err: xerr.Errorf(xerr.GetAuthenticatorError, "auth method %q is disabled", "github")}
			assertFailure(t, post(newRouter(t, facade), path, jsonBody, body), xerr.GetAuthenticatorError, "Unsupported login method")
		})
	}
}
