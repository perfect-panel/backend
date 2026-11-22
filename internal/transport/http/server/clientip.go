package httpserver

import (
	"fmt"
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	appconfig "github.com/perfect-panel/backend/internal/config"
)

// remoteIPHeaders are the headers a trusted reverse proxy names the client
// in, in the order they are consulted.
var remoteIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}

const (
	// TrustNoProxy is the TrustedProxies entry that believes no forwarding
	// header: every client is the connection's peer. It is for a server its
	// clients reach directly, and it cannot be combined with other entries.
	TrustNoProxy = "none"
	// TrustPrivateNetworks is the entry that stands for the default networks
	// (appconfig.DefaultTrustedProxies) inside a longer list, so a public
	// proxy can be added without spelling the private ranges out.
	TrustPrivateNetworks = "private"
)

// ParseTrustedProxies parses the configured reverse proxies, each an IP
// address or a CIDR, into the networks whose X-Forwarded-For and X-Real-IP
// headers are believed. An empty configuration means the default networks,
// the entry "private" stands for them and ["none"] trusts nothing. It
// reports every entry that is none of these, so a typo does not silently
// widen or narrow the trust.
func ParseTrustedProxies(entries []string) ([]*net.IPNet, error) {
	var names []string
	for _, entry := range entries {
		if entry = strings.TrimSpace(entry); entry != "" {
			names = append(names, entry)
		}
	}
	if len(names) == 0 {
		names = appconfig.DefaultTrustedProxies
	}
	var networks []*net.IPNet
	var invalid []string
	none := false
	for _, entry := range names {
		switch strings.ToLower(entry) {
		case TrustNoProxy:
			none = true
		case TrustPrivateNetworks:
			// The defaults are literals; a parse failure there is a
			// programming error, not an operator's typo.
			defaults, _ := parseNetworks(appconfig.DefaultTrustedProxies)
			networks = append(networks, defaults...)
		default:
			parsed, ok := parseNetworks([]string{entry})
			if !ok {
				invalid = append(invalid, entry)
				continue
			}
			networks = append(networks, parsed...)
		}
	}
	if none {
		if len(names) > 1 {
			return nil, fmt.Errorf("trusted proxies: %q cannot be combined with other entries, got %q", TrustNoProxy, names)
		}
		return nil, nil
	}
	if len(invalid) > 0 {
		return networks, fmt.Errorf("trusted proxies %q are neither IP addresses nor CIDRs", invalid)
	}
	return networks, nil
}

// parseNetworks parses IP addresses and CIDRs; ok is false when an entry is
// neither.
func parseNetworks(entries []string) (networks []*net.IPNet, ok bool) {
	ok = true
	for _, entry := range entries {
		if _, network, err := net.ParseCIDR(entry); err == nil {
			networks = append(networks, network)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			ok = false
			continue
		}
		bits := 8 * net.IPv6len
		if ip.To4() != nil {
			ip = ip.To4()
			bits = 8 * net.IPv4len
		}
		networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return networks, ok
}

// clientIPFunc resolves the client address of a request: the connection's
// remote address, unless that address is one of the trusted proxies, in
// which case the rightmost address of X-Forwarded-For (or X-Real-IP) that is
// not itself a trusted proxy names the client. With no trusted proxies
// (["none"]) no header is believed at all, so a client cannot choose the
// address the rate limits, audit logs and device records see.
func clientIPFunc(trusted []*net.IPNet) app.ClientIP {
	return app.ClientIPWithOption(app.ClientIPOptions{RemoteIPHeaders: remoteIPHeaders, TrustedCIDRs: trusted})
}
