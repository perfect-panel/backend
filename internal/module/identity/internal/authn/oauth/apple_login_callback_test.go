package oauth

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/redis/go-redis/v9"
)

func Test_appleLoginRedirect_preserves_found_location_when_state_is_valid(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "code value", State: "state value"}

	// When
	redirect := appleLoginRedirect("https://panel.example/callback", req, http.StatusFound)

	// Then
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusFound)
	}
	if redirect.Location != "https://panel.example/callback?code=code+value&method=apple&state=state+value" {
		t.Fatalf("location = %q", redirect.Location)
	}
}

func Test_appleLoginRedirect_encodes_query_components_when_state_or_code_have_delimiters(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "code with spaces&symbols=1&2", State: "state?x=1&y=2"}

	// When
	redirect := appleLoginRedirect("https://panel.example/callback?from=apple", req, http.StatusFound)

	// Then
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusFound)
	}
	if redirect.Location != "https://panel.example/callback?code=code+with+spaces%26symbols%3D1%262&from=apple&method=apple&state=state%3Fx%3D1%26y%3D2" {
		t.Fatalf("location = %q", redirect.Location)
	}
}

func Test_appleLoginRedirect_preserves_temporary_redirect_when_state_is_invalid(t *testing.T) {
	// Given
	req := &dto.AppleLoginCallbackRequest{Code: "ignored", State: "ignored"}

	// When
	redirect := appleLoginRedirect("https://panel.example", req, http.StatusTemporaryRedirect)

	// Then
	if redirect.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusTemporaryRedirect)
	}
	if redirect.Location != "https://panel.example" {
		t.Fatalf("location = %q", redirect.Location)
	}
}

func newAppleCallback(t *testing.T, fallback string) (*Service, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewService(Deps{Redis: client, Config: func() Config { return Config{SiteHost: fallback} }}), client
}

// issueAppleState stores a sign-in state whose redirect is redirect.
func issueAppleState(t *testing.T, client *redis.Client, redirect string) string {
	t.Helper()
	state, err := oauthstate.Issue(context.Background(), client, "apple", oauthstate.LoginScope(), redirect, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// postedTo is the context of Apple's form post to the API host host, as
// the trace middleware records it.
func postedTo(host string) context.Context {
	return context.WithValue(context.Background(), requestctx.CtxKeyRequestHost, host)
}

// The callback hands the code and state on to the stored redirect without
// redeeming the state: the sign-in that follows redeems it.
func TestAppleLoginCallbackFollowsStoredRedirectOnTheSiteHost(t *testing.T) {
	svc, client := newAppleCallback(t, "https://panel.example")
	state := issueAppleState(t, client, "https://panel.example/callback")

	redirect, err := svc.AppleLoginCallback(context.Background(), &dto.AppleLoginCallbackRequest{State: state, Code: "code-1"})
	if err != nil {
		t.Fatalf("AppleLoginCallback error = %v", err)
	}
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", redirect.StatusCode, http.StatusFound)
	}
	if want := "https://panel.example/callback?code=code-1&method=apple&state=" + state; redirect.Location != want {
		t.Fatalf("location = %q, want %q", redirect.Location, want)
	}
	if _, err := oauthstate.Consume(context.Background(), client, "apple", oauthstate.LoginScope(), state, ""); err != nil {
		t.Fatalf("the callback redeemed the state the sign-in needs: %v", err)
	}
}

// The redirect carries the authorization code and state, a credential; the
// log names where the browser is sent, not what it carries.
func TestAppleLoginCallbackLogsTheRedirectWithoutTheCredential(t *testing.T) {
	svc, client := newAppleCallback(t, "https://panel.example")
	state := issueAppleState(t, client, "https://panel.example/callback?from=apple")
	logs := logtest.NewCollector(t)

	if _, err := svc.AppleLoginCallback(context.Background(), &dto.AppleLoginCallbackRequest{State: state, Code: "c0de-secret"}); err != nil {
		t.Fatalf("AppleLoginCallback error = %v", err)
	}
	entries := logs.String()
	if strings.Contains(entries, "c0de-secret") || strings.Contains(entries, state) || strings.Contains(entries, "from=apple") {
		t.Fatalf("logs = %q, want the code, state and query kept out", entries)
	}
	if !strings.Contains(entries, `"host":"panel.example"`) || !strings.Contains(entries, `"path":"/callback"`) {
		t.Fatalf("logs = %q, want the redirect's host and path", entries)
	}
}

func TestAppleLoginCallbackRejectsStoredRedirectOffTheSiteHost(t *testing.T) {
	svc, client := newAppleCallback(t, "https://panel.example")
	state := issueAppleState(t, client, "https://evil.example/phish")

	redirect, err := svc.AppleLoginCallback(context.Background(), &dto.AppleLoginCallbackRequest{State: state, Code: "code-1"})
	if err != nil {
		t.Fatalf("AppleLoginCallback error = %v", err)
	}
	if redirect.StatusCode != http.StatusTemporaryRedirect || redirect.Location != "https://panel.example" {
		t.Fatalf("redirect = %#v, want temporary fallback redirect", redirect)
	}
}

func TestAppleLoginCallbackSendsAnUnknownStateToTheSiteHost(t *testing.T) {
	svc, _ := newAppleCallback(t, "https://panel.example/fallback")

	redirect, err := svc.AppleLoginCallback(context.Background(), &dto.AppleLoginCallbackRequest{State: "missing"})
	if err != nil {
		t.Fatalf("AppleLoginCallback error = %v", err)
	}
	if redirect.StatusCode != http.StatusTemporaryRedirect || redirect.Location != "https://panel.example/fallback" {
		t.Fatalf("redirect = %#v, want temporary fallback redirect", redirect)
	}
}

// Without a configured site host the callback fails closed. The stored
// redirect is pinned to the host Apple posted to (the API host the victim's
// browser is sent to by Apple, which an attacker does not choose): a
// redirect an attacker planted for their own host never receives the
// victim's code, and the browser is sent to the API's root instead. Without
// a request host either, nothing is trusted.
func TestAppleLoginCallbackWithoutASiteHostNeverLeaksTheCodeOffTheRequestHost(t *testing.T) {
	for name, tc := range map[string]struct {
		redirect, requestHost, wantLocation string
	}{
		"attacker's redirect":          {"https://evil.example/steal", "api.panel.example", "/"},
		"attacker's redirect, no host": {"https://evil.example/steal", "", "/"},
		"panel's redirect, no host":    {"https://panel.example/callback", "", "/"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, client := newAppleCallback(t, "")
			state := issueAppleState(t, client, tc.redirect)
			ctx := context.Background()
			if tc.requestHost != "" {
				ctx = postedTo(tc.requestHost)
			}
			redirect, err := svc.AppleLoginCallback(ctx, &dto.AppleLoginCallbackRequest{State: state, Code: "victim-code"})
			if err != nil {
				t.Fatalf("AppleLoginCallback error = %v", err)
			}
			if redirect.StatusCode != http.StatusTemporaryRedirect || redirect.Location != tc.wantLocation {
				t.Fatalf("redirect = %#v, want 307 to %q", redirect, tc.wantLocation)
			}
			if strings.Contains(redirect.Location, "victim-code") {
				t.Fatalf("redirect %q carries the victim's code", redirect.Location)
			}
		})
	}
}

// Without a configured site host, a redirect on the host Apple posted to, a
// subdomain or a parent domain of it, still completes: a deployment that
// never configured its site host keeps working on its own hosts.
func TestAppleLoginCallbackWithoutASiteHostFollowsRedirectsOnTheRequestHost(t *testing.T) {
	for name, tc := range map[string]struct{ redirect, requestHost string }{
		"same host":     {"https://panel.example/callback", "panel.example"},
		"parent domain": {"https://panel.example/callback", "api.panel.example:443"},
		"subdomain":     {"https://app.panel.example/callback", "panel.example"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, client := newAppleCallback(t, "")
			state := issueAppleState(t, client, tc.redirect)
			redirect, err := svc.AppleLoginCallback(postedTo(tc.requestHost), &dto.AppleLoginCallbackRequest{State: state, Code: "code-1"})
			if err != nil {
				t.Fatalf("AppleLoginCallback error = %v", err)
			}
			if want := tc.redirect + "?code=code-1&method=apple&state=" + state; redirect.StatusCode != http.StatusFound || redirect.Location != want {
				t.Fatalf("redirect = %#v, want 302 to %q", redirect, want)
			}
		})
	}
}
