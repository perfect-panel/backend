package user

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	account "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The OAuth binding handlers run over the real identity facade, so the
// binding's redirect is validated and its callback redeemed as in
// production; the tests pin that behaviour as the client sees it.

const (
	bindOAuthPath         = "/v1/public/user/bind_oauth"
	bindOAuthCallbackPath = "/v1/public/user/bind_oauth/callback"
	// telegramBotToken signs the Telegram Login Widget's results.
	telegramBotToken = "123456:binding-test-token"
)

// bindingFixture is the facade over the module's test store with Apple,
// GitHub and Telegram enabled, and the signed-in account.
type bindingFixture struct {
	*identitytest.Env
	facade identity.Service
	owner  *account.User
}

func newBindingFixture(t *testing.T) *bindingFixture {
	t.Helper()
	env := identitytest.New(t)
	env.EnableMethod(t, "apple", `{"client_id":"com.example.panel","redirect_url":"https://api.panel.example"}`)
	env.EnableMethod(t, "github", "{}")
	env.EnableMethod(t, "telegram", `{"bot_token":"`+telegramBotToken+`"}`)
	enabled := true
	owner := &account.User{Enable: &enabled}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	facade := identity.New(identity.Deps{
		Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
		Cache: env.Store.UserCache(), Logs: env.Store.Log(), Auths: env.Store.Auth(), Store: env.Store, Redis: env.Redis,
		AuthConfig: func() identity.AuthSnapshot { return identity.AuthSnapshot{SiteHost: "https://panel.example"} },
	})
	return &bindingFixture{Env: env, facade: facade, owner: owner}
}

// telegramResult is the Telegram Login Widget's signed result for the
// Telegram account telegramID, as the widget hands it to the page.
func telegramResult(t *testing.T, telegramID int64) string {
	t.Helper()
	authDate := time.Now().Unix()
	key := sha256.Sum256([]byte(telegramBotToken))
	mac := hmac.New(sha256.New, key[:])
	// The data-check string: every signed field but the hash, as key=value
	// lines in key order.
	checkString := fmt.Sprintf("auth_date=%d\nfirst_name=Owner\nid=%d", authDate, telegramID)
	mac.Write([]byte(checkString))
	payload, err := json.Marshal(map[string]any{
		"id": telegramID, "first_name": "Owner", "auth_date": authDate, "hash": hex.EncodeToString(mac.Sum(nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// Binding starts with the same pinned redirect as signing in: an Apple
// binding may only come back to the site host, and a refused redirect gets
// no authorization URL.
func TestBindOAuthPinsTheRedirectToTheSiteHost(t *testing.T) {
	f := newBindingFixture(t)
	for redirect, allowed := range map[string]bool{
		"https://panel.example/bind/apple":     true,
		"https://app.panel.example/bind/apple": true,
		"https://evil.example/bind/apple":      false,
	} {
		t.Run(redirect, func(t *testing.T) {
			w := callAs(t, f.owner, http.MethodPost, bindOAuthPath, BindOAuthHandler(f.facade), `{"method":"apple","redirect":"`+redirect+`"}`)
			code, msg, data := envelope(t, w)
			if !allowed {
				if code != xerr.InvalidParams || msg != "Param Error" || data != nil {
					t.Fatalf("reply = {%d %q %s}, want the redirect refused", code, msg, data)
				}
				return
			}
			var started dto.BindOAuthResponse
			if err := json.Unmarshal(data, &started); err != nil || code != xerr.SUCCESS {
				t.Fatalf("reply = {%d %q %s} (%v), want the authorization URL", code, msg, data, err)
			}
			authorize, err := url.Parse(started.Redirect)
			if err != nil || authorize.Host != "appleid.apple.com" || authorize.Query().Get("state") == "" {
				t.Fatalf("authorization URL = %q (%v)", started.Redirect, err)
			}
		})
	}
}

// The binding callback hands the provider's callback object to the facade,
// which binds the identity the provider vouches for to the account the
// request context carries; the body cannot name another.
func TestBindOAuthCallbackBindsTheIdentityToTheSignedInAccount(t *testing.T) {
	f := newBindingFixture(t)
	body := `{"method":"telegram","user_id":99,"callback":{"tgAuthResult":"` + telegramResult(t, 583231) + `"}}`
	code, msg, data := envelope(t, callAs(t, f.owner, http.MethodPost, bindOAuthCallbackPath, BindOAuthCallbackHandler(f.facade), body))
	if code != xerr.SUCCESS || msg != "success" || data != nil {
		t.Fatalf("reply = {%d %q %s}, want success without data", code, msg, data)
	}
	identities := f.Identities(t, f.owner.Id)
	if len(identities) != 1 || identities[0].AuthType != "telegram" || identities[0].AuthIdentifier != "583231" || !identities[0].Verified {
		t.Fatalf("identities of the signed-in account = %+v, want the verified Telegram one", identities)
	}
}

// A callback the facade cannot redeem and one without a signed-in account
// are refused with the code the client sees, and bind nothing.
func TestBindOAuthCallbackRefusesWhatItCannotRedeem(t *testing.T) {
	f := newBindingFixture(t)
	for name, tc := range map[string]struct {
		signedIn *account.User
		body     string
		code     uint32
		msg      string
	}{
		"unknown state": {f.owner, `{"method":"github","callback":{"code":"github-code","state":"never-issued"}}`, xerr.OAuthStateInvalid, "OAuth state is invalid or expired"},
		"not an object": {f.owner, `{"method":"github","callback":"github-code"}`, xerr.InvalidParams, "Param Error"},
		"not signed in": {nil, `{"method":"github","callback":{"code":"github-code","state":"never-issued"}}`, xerr.InvalidAccess, "Invalid access"},
	} {
		t.Run(name, func(t *testing.T) {
			w := callAs(t, tc.signedIn, http.MethodPost, bindOAuthCallbackPath, BindOAuthCallbackHandler(f.facade), tc.body)
			if code, msg, data := envelope(t, w); code != tc.code || msg != tc.msg || data != nil {
				t.Fatalf("reply = {%d %q %s}, want {%d %q} without data", code, msg, data, tc.code, tc.msg)
			}
			if identities := f.Identities(t, f.owner.Id); len(identities) != 0 {
				t.Fatalf("identities = %+v, want none bound", identities)
			}
		})
	}
}

// A request that does not parse, or names a provider the request
// validation does not know, or misses a field, is a parameter error: it
// neither starts a binding nor binds anything. The validator names the
// field; a decoding error's text is the JSON decoder's own.
func TestBindOAuthHandlersRejectInvalidRequests(t *testing.T) {
	f := newBindingFixture(t)
	for name, tc := range map[string]struct {
		path, body, msg string
	}{
		"bind unparsable":           {bindOAuthPath, `{"method":`, ""},
		"bind unknown provider":     {bindOAuthPath, `{"method":"myspace","redirect":"https://panel.example/bind"}`, "Method must be one of [google apple telegram github facebook]"},
		"bind without redirect":     {bindOAuthPath, `{"method":"apple"}`, "Redirect is a required field"},
		"callback unparsable":       {bindOAuthCallbackPath, `{"method":"github","callback":{`, ""},
		"callback unknown provider": {bindOAuthCallbackPath, `{"method":"myspace","callback":{"code":"c"}}`, "Method must be one of [google apple telegram github facebook]"},
		"callback without callback": {bindOAuthCallbackPath, `{"method":"github"}`, "Callback is a required field"},
	} {
		t.Run(name, func(t *testing.T) {
			handler := BindOAuthHandler(f.facade)
			if tc.path == bindOAuthCallbackPath {
				handler = BindOAuthCallbackHandler(f.facade)
			}
			code, msg, data := envelope(t, callAs(t, f.owner, http.MethodPost, tc.path, handler, tc.body))
			if code != xerr.InvalidParams || (tc.msg != "" && msg != tc.msg) || data != nil {
				t.Fatalf("reply = {%d %q %s}, want code %d with %q", code, msg, data, xerr.InvalidParams, tc.msg)
			}
			if keys := f.Mini.Keys(); len(keys) != 0 {
				t.Fatalf("an invalid request stored %v", keys)
			}
			if identities := f.Identities(t, f.owner.Id); len(identities) != 0 {
				t.Fatalf("identities = %+v, want none bound", identities)
			}
		})
	}
}
