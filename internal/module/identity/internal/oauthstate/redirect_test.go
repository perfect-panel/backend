package oauthstate

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
)

// requestTo is a request context whose Host header is host, as the trace
// middleware records it.
func requestTo(host string) context.Context {
	return context.WithValue(context.Background(), requestctx.CtxKeyRequestHost, host)
}

// A redirect stays on the configured site host or one of its subdomains,
// whatever form the administrator wrote the host in, and is a web URL.
func TestValidateRedirectPinsToTheConfiguredSiteHost(t *testing.T) {
	tests := []struct {
		name     string
		redirect string
		siteHost string
		wantErr  bool
	}{
		{name: "same host passes", redirect: "https://panel.example/callback", siteHost: "https://panel.example", wantErr: false},
		{name: "subdomain passes", redirect: "https://app.panel.example/cb", siteHost: "panel.example", wantErr: false},
		{name: "bare domain site host passes", redirect: "http://panel.example:3000/cb", siteHost: "panel.example", wantErr: false},
		{name: "host is case insensitive", redirect: "https://Panel.Example/cb", siteHost: "https://panel.example", wantErr: false},
		{name: "foreign host rejected", redirect: "https://evil.example/phish", siteHost: "https://panel.example", wantErr: true},
		{name: "suffix lookalike rejected", redirect: "https://evilpanel.example/cb", siteHost: "panel.example", wantErr: true},
		{name: "parent of the site host rejected", redirect: "https://example/cb", siteHost: "panel.example", wantErr: true},
		{name: "javascript scheme rejected", redirect: "javascript:alert(1)", siteHost: "panel.example", wantErr: true},
		{name: "data scheme rejected", redirect: "data:text/html,x", siteHost: "panel.example", wantErr: true},
		{name: "relative redirect rejected", redirect: "/local/path", siteHost: "panel.example", wantErr: true},
		{name: "empty redirect rejected", redirect: "", siteHost: "panel.example", wantErr: true},
		{name: "scheme-relative redirect rejected", redirect: "//evil.example/cb", siteHost: "panel.example", wantErr: true},
		{name: "userinfo lookalike rejected", redirect: "https://panel.example@evil.example/cb", siteHost: "panel.example", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRedirect(tt.redirect, SitePin(tt.siteHost))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRedirect(%q, site %q) error = %v, wantErr %v", tt.redirect, tt.siteHost, err, tt.wantErr)
			}
		})
	}
}

// Without a configured site host the check fails closed: no redirect is
// allowed on the site pin, and a redirect passes on the request pin only
// when it stays on the host the request was made to, a subdomain of it or a
// parent domain of it (so an API on api.panel.example returns to
// panel.example). An attacker's host passes on neither.
func TestValidateRedirectWithoutASiteHostFailsClosed(t *testing.T) {
	for _, redirect := range []string{"https://anywhere.example/cb", "https://panel.example/cb"} {
		if err := ValidateRedirect(redirect, SitePin("")); !errors.Is(err, ErrUnpinned) {
			t.Fatalf("ValidateRedirect(%q, empty site pin) = %v, want ErrUnpinned", redirect, err)
		}
		if err := ValidateRedirect(redirect, RequestPin(context.Background())); !errors.Is(err, ErrUnpinned) {
			t.Fatalf("ValidateRedirect(%q, no request host) = %v, want ErrUnpinned", redirect, err)
		}
	}

	tests := []struct {
		name, redirect, requestHost string
		wantErr                     bool
	}{
		{name: "request host passes", redirect: "https://panel.example/cb", requestHost: "panel.example"},
		{name: "request host with port passes", redirect: "https://panel.example/cb", requestHost: "panel.example:8080"},
		{name: "subdomain of the request host passes", redirect: "https://app.panel.example/cb", requestHost: "panel.example"},
		{name: "parent domain of the request host passes", redirect: "https://panel.example/cb", requestHost: "api.panel.example"},
		{name: "localhost passes", redirect: "http://localhost:3000/cb", requestHost: "localhost:8080"},
		{name: "top-level parent rejected", redirect: "https://example/cb", requestHost: "api.panel.example", wantErr: true},
		{name: "attacker host rejected", redirect: "https://evil.example/cb", requestHost: "panel.example", wantErr: true},
		{name: "sibling host rejected", redirect: "https://evil.panel.example/cb", requestHost: "api.panel.example", wantErr: true},
		{name: "lookalike rejected", redirect: "https://evilpanel.example/cb", requestHost: "panel.example", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pin := RequestPin(requestTo(tt.requestHost))
			if !pin.Configured() || !pin.FromRequest() {
				t.Fatalf("RequestPin(%q) = %+v, want a request pin", tt.requestHost, pin)
			}
			err := ValidateRedirect(tt.redirect, pin)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRedirect(%q, request host %q) error = %v, wantErr %v", tt.redirect, tt.requestHost, err, tt.wantErr)
			}
		})
	}
}

// The parent-domain allowance belongs to the request pin only: a configured
// site host is taken as written.
func TestParentDomainsPassOnlyOnTheRequestPin(t *testing.T) {
	if err := ValidateRedirect("https://panel.example/cb", SitePin("api.panel.example")); err == nil {
		t.Fatal("a parent of the configured site host was allowed")
	}
	if err := ValidateRedirect("https://panel.example/cb", RequestPin(requestTo("api.panel.example"))); err != nil {
		t.Fatalf("a parent of the request host was refused: %v", err)
	}
}

// The site host wins over the request host whenever it is configured.
func TestResolvePinPrefersTheSiteHost(t *testing.T) {
	ctx := requestTo("evil.example")
	if pin := ResolvePin(ctx, "https://panel.example"); pin.Host() != "panel.example" || pin.FromRequest() {
		t.Fatalf("ResolvePin with a site host = %+v, want the site pin", pin)
	}
	if pin := ResolvePin(ctx, ""); pin.Host() != "evil.example" || !pin.FromRequest() {
		t.Fatalf("ResolvePin without a site host = %+v, want the request pin", pin)
	}
	if pin := ResolvePin(context.Background(), ""); pin.Configured() {
		t.Fatalf("ResolvePin with neither = %+v, want none", pin)
	}
}
