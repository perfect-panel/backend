package oauthstate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
)

// ErrUnpinned reports that no host is known to pin a redirect to: the
// administrator configured no site host and the request's own host is not
// available either. The check fails closed, so the redirect is refused.
var ErrUnpinned = errors.New("no site host is configured to pin the OAuth redirect to; set the site host in the system settings")

// Pin is the host a client-supplied OAuth redirect must stay on. It is the
// configured site host when the administrator set one, or else the host the
// request was made to, so a deployment that never configured its site host
// keeps working for redirects to its own host and nothing else.
type Pin struct {
	host string
	// request marks a pin taken from the request's Host header rather than
	// the configured site host.
	request bool
}

// SitePin pins redirects to the configured site host, which administrators
// record either as a bare domain or as a full URL; the zero Pin when it is
// empty or malformed.
func SitePin(siteHost string) Pin {
	return Pin{host: siteHostname(siteHost)}
}

// RequestPin pins redirects to the host the request was made to, which the
// trace middleware records in ctx; the zero Pin when ctx carries none. The
// Host header is the client's, so this pin only proves that the redirect
// stays on a host the server itself answers for.
func RequestPin(ctx context.Context) Pin {
	if ctx == nil {
		return Pin{}
	}
	host, _ := ctx.Value(requestctx.CtxKeyRequestHost).(string)
	return Pin{host: hostname(host), request: true}
}

// ResolvePin returns the site pin when a site host is configured, otherwise
// the request pin.
func ResolvePin(ctx context.Context, siteHost string) Pin {
	if pin := SitePin(siteHost); pin.Configured() {
		return pin
	}
	return RequestPin(ctx)
}

// Configured reports whether the pin names a host.
func (p Pin) Configured() bool { return p.host != "" }

// FromRequest reports whether the pin is the request's own host rather than
// the configured site host.
func (p Pin) FromRequest() bool { return p.request }

// Host is the pinned hostname, lowercase and without a port.
func (p Pin) Host() string { return p.host }

// ValidateRedirect checks a client-supplied OAuth redirect target before it
// is stored as state and later used as a browser redirect. The scheme must
// be web-safe and the host must stay on the pin: the pinned host or one of
// its subdomains. A pin taken from the request also admits a parent domain
// of the request host (with at least two labels), so an API served at
// api.panel.example may send the browser back to panel.example without any
// configuration; a configured site host is taken as the administrator wrote
// it. Without a pin the check fails closed with ErrUnpinned.
func ValidateRedirect(redirect string, pin Pin) error {
	u, err := url.Parse(strings.TrimSpace(redirect))
	if err != nil {
		return fmt.Errorf("parse redirect %q: %w", redirect, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("redirect scheme %q is not allowed", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("redirect %q has no host", redirect)
	}
	if !pin.Configured() {
		return ErrUnpinned
	}
	if host == pin.host || isSubdomain(host, pin.host) {
		return nil
	}
	if pin.request && isSubdomain(pin.host, host) && strings.Count(host, ".") >= 1 {
		return nil
	}
	return fmt.Errorf("redirect host %q does not match the pinned host %q", host, pin.host)
}

// isSubdomain reports whether host is a subdomain of parent.
func isSubdomain(host, parent string) bool {
	return strings.HasSuffix(host, "."+parent)
}

// siteHostname extracts the hostname from the configured site host, which
// administrators record either as a bare domain or as a full URL.
func siteHostname(siteHost string) string {
	raw := strings.TrimSpace(siteHost)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// hostname strips the port from a Host header value.
func hostname(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}
