package gateway

import (
	"net"
	"net/url"
	"strings"

	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// NotifyURL builds the callback URL of a payment method; it carries the
// method's secret token in its path. The base is the method's Domain, else
// the public site host. The server's listen address is never used: it names
// an interface to bind, not an address a gateway can reach. Neither is the
// request Host header, which the client controls: deriving the callback from
// it would let a caller send the notification, token included, to a server
// of its choice.
func NotifyURL(method *paymentEntity.Payment, siteHost string) (string, error) {
	base, err := notifyBaseURL(method, siteHost)
	if err != nil {
		return "", err
	}
	return base + "/v1/notify/" + method.Platform + "/" + method.Token, nil
}

func notifyBaseURL(method *paymentEntity.Payment, siteHost string) (string, error) {
	if base := strings.TrimSuffix(strings.TrimSpace(method.Domain), "/"); base != "" {
		return base, nil
	}
	if base, ok := publicBaseURL(firstLine(siteHost)); ok {
		return base, nil
	}
	return "", xerr.Errorf(xerr.PaymentNotifyURLNotConfigured, "payment method %d has no domain and no site host is configured", method.Id)
}

// firstLine is the first line of a site host setting that lists several
// hosts one per line.
func firstLine(hosts string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(hosts), "\n")
	return first
}

// publicBaseURL turns a configured host, either a bare host[:port] or a full
// URL, into a callback base URL. Wildcard and loopback addresses are
// unreachable for a gateway and count as not configured.
func publicBaseURL(host string) (string, bool) {
	raw := strings.TrimSpace(host)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "localhost" {
		return "", false
	}
	if ip := net.ParseIP(hostname); ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
		return "", false
	}
	return strings.TrimSuffix(parsed.Scheme+"://"+parsed.Host+parsed.EscapedPath(), "/"), true
}
