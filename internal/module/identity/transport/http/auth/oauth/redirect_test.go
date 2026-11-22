package oauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// These tests run the handlers over the real identity facade, so redirect
// targets are validated as in production (oauthstate.ValidateRedirect) and
// states issued and read back from Redis: they pin what the browser sees of
// that validation.

// siteHost is the configured site host the pinned OAuth redirects must stay
// on, and the Apple callback's fallback target.
const siteHost = "https://panel.example"

// appleConfig is the stored Apple configuration: the authorization URL
// sends Apple's form post to redirect_url's callback route.
const appleConfig = `{"client_id":"com.example.panel","redirect_url":"https://api.panel.example"}`

// newFacade builds the identity facade over the module's test store with
// Apple and GitHub sign-in enabled.
func newFacade(t *testing.T) (identity.Service, *identitytest.Env) {
	t.Helper()
	env := identitytest.New(t)
	env.EnableMethod(t, "apple", appleConfig)
	env.EnableMethod(t, "github", "{}")
	return identity.New(identity.Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
		AuthConfig: func() identity.AuthSnapshot { return identity.AuthSnapshot{SiteHost: siteHost} },
	}), env
}

// startLogin runs the login for method and redirect and returns the
// authorization URL it answered with.
func startLogin(t *testing.T, facade oauthFacade, method, redirect string) *url.URL {
	t.Helper()
	body := `{"method":"` + method + `","redirect":"` + redirect + `"}`
	var data dto.OAuthLoginResponse
	assertSuccess(t, post(newRouter(t, facade), loginPath, jsonBody, body), &data)
	authorize, err := url.Parse(data.Redirect)
	if err != nil {
		t.Fatalf("authorization URL %q: %v", data.Redirect, err)
	}
	return authorize
}

// The browser's whole Apple sign-in through the two handlers: the login
// answers with Apple's authorization URL carrying a fresh state, and Apple's
// form post of that state and a code is redirected, with 302, to the page
// the login named, carrying the code and state on to the token exchange.
func TestAppleSignInRedirectsTheFormPostToThePageTheLoginNamed(t *testing.T) {
	facade, _ := newFacade(t)
	authorize := startLogin(t, facade, "apple", "https://panel.example/oauth/apple")
	query := authorize.Query()
	state := query.Get("state")
	if authorize.Scheme != "https" || authorize.Host != "appleid.apple.com" || authorize.Path != "/auth/authorize" || state == "" ||
		query.Get("redirect_uri") != "https://api.panel.example/v1/auth/oauth/callback/apple" || query.Get("response_mode") != "form_post" {
		t.Fatalf("authorization URL = %s", authorize)
	}

	w := post(newRouter(t, facade), appleCallbackPath, formPost,
		url.Values{"code": {"apple-code"}, "id_token": {"apple-id-token"}, "state": {state}}.Encode())
	want := "https://panel.example/oauth/apple?" + url.Values{"code": {"apple-code"}, "method": {"apple"}, "state": {state}}.Encode()
	if w.Code != http.StatusFound || w.Header().Get("Location") != want {
		t.Fatalf("callback answer = %d to %q, want 302 to %q", w.Code, w.Header().Get("Location"), want)
	}
}

// A redirect the callback would send the browser to must stay on the site
// host or one of its subdomains and be a web URL; the login refuses any
// other before it issues a state. A code-flow provider's redirect is left
// to the provider, which only returns to the redirect URI registered with
// it.
func TestOAuthLoginPinsTheRedirectOfMethodsThatRedirectTheBrowser(t *testing.T) {
	for _, tc := range []struct {
		method, redirect string
		allowed          bool
	}{
		{"apple", "https://panel.example/oauth/apple", true},
		{"apple", "https://app.panel.example/oauth/apple", true},
		{"apple", "http://panel.example/oauth/apple", true},
		{"apple", "https://evil.example/phish", false},
		{"apple", "https://panel.example.evil.example/phish", false},
		{"apple", "https://evilpanel.example/phish", false},
		{"apple", "https://panel.example@evil.example/phish", false},
		{"apple", "//evil.example/phish", false},
		{"apple", "javascript:alert(1)", false},
		{"apple", "", false},
		{"github", "https://evil.example/oauth/github", true},
	} {
		t.Run(tc.method+" "+tc.redirect, func(t *testing.T) {
			facade, env := newFacade(t)
			body := `{"method":"` + tc.method + `","redirect":"` + tc.redirect + `"}`
			w := post(newRouter(t, facade), loginPath, jsonBody, body)
			if !tc.allowed {
				assertFailure(t, w, xerr.InvalidParams, "Param Error")
				if keys := env.Mini.Keys(); len(keys) != 0 {
					t.Fatalf("a refused redirect stored %v", keys)
				}
				return
			}
			var data dto.OAuthLoginResponse
			assertSuccess(t, w, &data)
			if len(env.Mini.Keys()) != 1 {
				t.Fatalf("states = %v, want the one issued with the URL", env.Mini.Keys())
			}
		})
	}
}

// A method the administrator has not enabled cannot start a sign-in.
func TestOAuthLoginRefusesAMethodThatIsNotEnabled(t *testing.T) {
	facade, env := newFacade(t)
	w := post(newRouter(t, facade), loginPath, jsonBody, `{"method":"google","redirect":"https://panel.example/oauth/google"}`)
	assertFailure(t, w, xerr.GetAuthenticatorError, "Unsupported login method")
	if keys := env.Mini.Keys(); len(keys) != 0 {
		t.Fatalf("a refused login stored %v", keys)
	}
}

// Apple's form post of a state the server does not hold (never issued,
// expired or already redeemed), or of one whose stored redirect left the
// site host, sends the browser back to the site host with 307 and nothing
// of the callback.
func TestAppleCallbackFallsBackToTheSiteHost(t *testing.T) {
	facade, env := newFacade(t)
	planted, err := oauthstate.Issue(context.Background(), env.Redis, "apple", oauthstate.LoginScope(), "https://evil.example/phish", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, state := range map[string]string{"unknown state": "never-issued", "redirect off the site host": planted} {
		t.Run(name, func(t *testing.T) {
			w := post(newRouter(t, facade), appleCallbackPath, formPost, url.Values{"code": {"apple-code"}, "state": {state}}.Encode())
			if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != siteHost {
				t.Fatalf("callback answer = %d to %q, want 307 to %q", w.Code, w.Header().Get("Location"), siteHost)
			}
		})
	}
}

// newUnpinnedFacade is newFacade for a deployment that never configured its
// site host.
func newUnpinnedFacade(t *testing.T) (identity.Service, *identitytest.Env) {
	t.Helper()
	env := identitytest.New(t)
	env.EnableMethod(t, "apple", appleConfig)
	env.EnableMethod(t, "github", "{}")
	return identity.New(identity.Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
		AuthConfig: func() identity.AuthSnapshot { return identity.AuthSnapshot{} },
	}), env
}

// postTo sends body to target as the browser would to host: the trace
// middleware records the request's Host, which pins redirects while no site
// host is configured.
func postTo(t *testing.T, facade oauthFacade, host, target, contentType, body string) *ut.ResponseRecorder {
	t.Helper()
	logtest.Discard(t)
	h := server.New()
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		ctx = context.WithValue(ctx, requestctx.CtxKeyRequestHost, host)
		c.Next(requestmeta.With(ctx, requestMetadata))
	})
	h.POST(appleCallbackPath, AppleLoginCallbackHandler(facade))
	h.POST(loginPath, OAuthLoginHandler(facade))
	h.POST(tokenPath, OAuthLoginGetTokenHandler(facade))
	return ut.PerformRequest(h.Engine, http.MethodPost, target,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: contentType}, ut.Header{Key: "Host", Value: host})
}

// The account takeover the unpinned redirect allowed: on a deployment
// without a site host, an attacker starts an Apple sign-in naming their own
// host as the redirect and hands the authorization URL to the victim. The
// login now refuses the foreign redirect outright (nothing is stored), and
// a state planted with such a redirect sends Apple's form post of the
// victim's code to the API's root with 307, without the code. Redirects on
// the deployment's own host keep working.
func TestUnpinnedDeploymentNeverSendsTheCodeToAnAttackersHost(t *testing.T) {
	facade, env := newUnpinnedFacade(t)
	const apiHost = "api.panel.example"

	w := postTo(t, facade, apiHost, loginPath, jsonBody, `{"method":"apple","redirect":"https://evil.example/steal"}`)
	assertFailure(t, w, xerr.InvalidParams, "Param Error")
	if keys := env.Mini.Keys(); len(keys) != 0 {
		t.Fatalf("the attacker's login stored %v", keys)
	}
	// Without even a request host nothing pins the redirect: refused.
	w = postTo(t, facade, "", loginPath, jsonBody, `{"method":"apple","redirect":"https://panel.example/oauth/apple"}`)
	assertFailure(t, w, xerr.OAuthProviderMisconfigured, "OAuth provider is not configured correctly")

	planted, err := oauthstate.Issue(context.Background(), env.Redis, "apple", oauthstate.LoginScope(), "https://evil.example/steal", "")
	if err != nil {
		t.Fatal(err)
	}
	w = postTo(t, facade, apiHost, appleCallbackPath, formPost, url.Values{"code": {"victim-code"}, "state": {planted}}.Encode())
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/" {
		t.Fatalf("callback answer = %d to %q, want 307 to /", w.Code, w.Header().Get("Location"))
	}

	// The deployment's own hosts still complete the round trip.
	var data dto.OAuthLoginResponse
	assertSuccess(t, postTo(t, facade, apiHost, loginPath, jsonBody, `{"method":"apple","redirect":"https://panel.example/oauth/apple"}`), &data)
	authorize, err := url.Parse(data.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	state := authorize.Query().Get("state")
	w = postTo(t, facade, apiHost, appleCallbackPath, formPost, url.Values{"code": {"apple-code"}, "state": {state}}.Encode())
	want := "https://panel.example/oauth/apple?" + url.Values{"code": {"apple-code"}, "method": {"apple"}, "state": {state}}.Encode()
	if w.Code != http.StatusFound || w.Header().Get("Location") != want {
		t.Fatalf("callback answer = %d to %q, want 302 to %q", w.Code, w.Header().Get("Location"), want)
	}
}

// A sign-in started with a nonce is completed only by the client presenting
// the same nonce: the code and state an attacker obtained in their own
// browser, with their own nonce or none, do not sign a victim's browser in,
// and the state is spent by the refused attempt.
func TestOAuthLoginNonceBindsTheSignInToTheClient(t *testing.T) {
	facade, env := newFacade(t)
	start := func(nonce string) string {
		body := `{"method":"github","redirect":"https://panel.example/oauth/github"`
		if nonce != "" {
			body += `,"nonce":"` + nonce + `"`
		}
		var data dto.OAuthLoginResponse
		assertSuccess(t, post(newRouter(t, facade), loginPath, jsonBody, body+"}"), &data)
		authorize, err := url.Parse(data.Redirect)
		if err != nil {
			t.Fatal(err)
		}
		return authorize.Query().Get("state")
	}
	exchange := func(state, nonce string) *ut.ResponseRecorder {
		body := `{"method":"github","callback":{"code":"github-code","state":"` + state + `"}`
		if nonce != "" {
			body += `,"nonce":"` + nonce + `"`
		}
		return post(newRouter(t, facade), tokenPath, jsonBody, body+"}")
	}

	// The attacker's state, started with their nonce, in the victim's browser.
	assertFailure(t, exchange(start("attacker-nonce-0123456789"), ""), xerr.OAuthStateInvalid, "OAuth state is invalid or expired")
	assertFailure(t, exchange(start("attacker-nonce-0123456789"), "victim-nonce-0123456789"), xerr.OAuthStateInvalid, "OAuth state is invalid or expired")
	// The attacker's state, started without a nonce, in a nonce-aware browser.
	assertFailure(t, exchange(start(""), "victim-nonce-0123456789"), xerr.OAuthStateInvalid, "OAuth state is invalid or expired")
	if keys := env.Mini.Keys(); len(keys) != 0 {
		t.Fatalf("refused states were left behind: %v", keys)
	}
	// Once refused, the state is spent even for the right nonce.
	state := start("client-nonce-0123456789")
	assertFailure(t, exchange(state, "other-nonce-0123456789"), xerr.OAuthStateInvalid, "OAuth state is invalid or expired")
	assertFailure(t, exchange(state, "client-nonce-0123456789"), xerr.OAuthStateInvalid, "OAuth state is invalid or expired")
	// A nonce is bounded.
	e := decodeEnvelope(t, post(newRouter(t, facade), loginPath, jsonBody, `{"method":"github","redirect":"https://panel.example/oauth/github","nonce":"`+strings.Repeat("n", 129)+`"}`))
	if e.Code != xerr.InvalidParams {
		t.Fatalf("an over-long nonce: envelope = {%d %q}", e.Code, e.Msg)
	}
}

// The token exchange redeems the state the login issued, reading the code
// and state from the callback object: a state the server does not hold is
// refused, and so is a callback that is not an object.
func TestOAuthLoginGetTokenRefusesACallbackWithoutAnIssuedState(t *testing.T) {
	facade, _ := newFacade(t)
	for name, tc := range map[string]struct {
		body string
		code uint32
		msg  string
	}{
		"unknown state": {`{"method":"github","callback":{"code":"github-code","state":"never-issued"}}`, xerr.OAuthStateInvalid, "OAuth state is invalid or expired"},
		"no state":      {`{"method":"github","callback":{"code":"github-code"}}`, xerr.InvalidParams, "Param Error"},
		"not an object": {`{"method":"github","callback":"github-code"}`, xerr.InvalidParams, "Param Error"},
	} {
		t.Run(name, func(t *testing.T) {
			assertFailure(t, post(newRouter(t, facade), tokenPath, jsonBody, tc.body), tc.code, tc.msg)
		})
	}
}
