package httpserver

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	appconfig "github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/transport/http/routes"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// peerConn is a connection with a fixed peer address, so a request served
// without a listener has a real address for Hertz to resolve the client IP
// from.
type peerConn struct {
	*mock.Conn
	peer net.Addr
}

func (c peerConn) RemoteAddr() net.Addr { return c.peer }

// probeServer builds the server with trustedProxies and a probe route that
// answers with the client address the middleware resolved.
func probeServer(t *testing.T, trustedProxies []string) *Server {
	t.Helper()
	logtest.Discard(t)
	srv := newServer(Dependencies{
		Routes:         routes.Dependencies{Config: appconfig.Config{}, Platform: platform.New(platform.Deps{})},
		TrustedProxies: trustedProxies,
	}, []hertzconfig.Option{server.WithHostPorts("127.0.0.1:0"), server.WithDisablePrintRoute(true)})
	srv.Engine().GET("/probe", func(c context.Context, ctx *app.RequestContext) {
		metadata, _ := requestmeta.From(c)
		ctx.String(http.StatusOK, metadata.ClientIP)
	})
	return srv
}

// resolvedClientIP is the client address the server records for a request
// from peer carrying headers.
func resolvedClientIP(t *testing.T, srv *Server, peer string, headers map[string]string) string {
	t.Helper()
	ctx := srv.Engine().NewContext()
	ctx.SetConn(peerConn{Conn: mock.NewConn(""), peer: &net.TCPAddr{IP: net.ParseIP(peer), Port: 41000}})
	ctx.Request.SetRequestURI("/probe")
	ctx.Request.Header.SetMethod(http.MethodGet)
	for key, value := range headers {
		ctx.Request.Header.Set(key, value)
	}
	srv.Engine().ServeHTTP(context.Background(), ctx)
	if ctx.Response.StatusCode() != http.StatusOK {
		t.Fatalf("probe status = %d", ctx.Response.StatusCode())
	}
	return string(ctx.Response.Body())
}

// With ["none"] the forwarding headers are ignored: the client is the
// connection's peer, whatever X-Forwarded-For or X-Real-IP claim. The rate
// limits, audit logs and device records keyed by the address cannot be
// steered by the client.
func TestClientIPIgnoresForwardingHeadersWithoutTrustedProxies(t *testing.T) {
	app := probeServer(t, []string{"none"})
	for name, headers := range map[string]map[string]string{
		"no headers":      {},
		"x-forwarded-for": {"X-Forwarded-For": "198.51.100.7"},
		"x-real-ip":       {"X-Real-IP": "198.51.100.7"},
		"both":            {"X-Forwarded-For": "198.51.100.7, 198.51.100.8", "X-Real-IP": "198.51.100.9"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := resolvedClientIP(t, app, "203.0.113.5", headers); got != "203.0.113.5" {
				t.Fatalf("client IP = %q, want the peer 203.0.113.5", got)
			}
		})
	}
}

// By default the loopback interface and the private networks are trusted:
// nginx on the same host, a Docker bridge or an internal load balancer name
// the client in X-Forwarded-For without configuration, while a public peer
// forwarding a header is still taken at its own address.
func TestClientIPTrustsLoopbackAndPrivateNetworksByDefault(t *testing.T) {
	app := probeServer(t, nil)
	for name, tc := range map[string]struct {
		peer    string
		headers map[string]string
		want    string
	}{
		"loopback nginx":          {"127.0.0.1", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		"docker bridge":           {"172.17.0.1", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		"10/8 load balancer":      {"10.1.2.3", map[string]string{"X-Real-IP": "198.51.100.7"}, "198.51.100.7"},
		"192.168 proxy":           {"192.168.1.10", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		"public peer forwarding":  {"203.0.113.5", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "203.0.113.5"},
		"loopback without header": {"127.0.0.1", map[string]string{}, "127.0.0.1"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := resolvedClientIP(t, app, tc.peer, tc.headers); got != tc.want {
				t.Fatalf("client IP = %q, want %q", got, tc.want)
			}
		})
	}
}

// An empty configuration is the default networks; "private" stands for them
// in a longer list; "none" trusts nothing and refuses company.
func TestParseTrustedProxiesKeywords(t *testing.T) {
	defaults, err := ParseTrustedProxies(nil)
	if err != nil || len(defaults) != len(appconfig.DefaultTrustedProxies) {
		t.Fatalf("ParseTrustedProxies(nil) = %v, %v; want the %d default networks", defaults, err, len(appconfig.DefaultTrustedProxies))
	}
	blank, err := ParseTrustedProxies([]string{" ", ""})
	if err != nil || len(blank) != len(defaults) {
		t.Fatalf("blank entries = %v, %v; want the defaults", blank, err)
	}
	extended, err := ParseTrustedProxies([]string{"Private", "203.0.113.10"})
	if err != nil || len(extended) != len(defaults)+1 {
		t.Fatalf("private + address = %v, %v; want the defaults plus one", extended, err)
	}
	none, err := ParseTrustedProxies([]string{"NONE"})
	if err != nil || none != nil {
		t.Fatalf("none = %v, %v; want no networks and no error", none, err)
	}
	if _, err := ParseTrustedProxies([]string{"none", "10.0.0.0/8"}); err == nil {
		t.Fatal("none combined with a network must be refused")
	}
}

// A connection from a trusted proxy names the client in X-Forwarded-For:
// the rightmost address that is not itself a trusted proxy, so a client
// prepending addresses of its own does not choose the answer. A connection
// from anywhere else keeps its peer address whatever it forwards.
func TestClientIPHonoursForwardingHeadersFromTrustedProxies(t *testing.T) {
	app := probeServer(t, []string{"10.0.0.0/8", "192.0.2.1"})
	for name, tc := range map[string]struct {
		peer    string
		headers map[string]string
		want    string
	}{
		"proxy forwards the client":             {"10.1.2.3", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		"single trusted address":                {"192.0.2.1", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		"chain of proxies, rightmost untrusted": {"10.1.2.3", map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.7, 10.9.9.9"}, "198.51.100.7"},
		"client forges an inner address":        {"10.1.2.3", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		"x-real-ip when no forwarded-for":       {"10.1.2.3", map[string]string{"X-Real-IP": "198.51.100.7"}, "198.51.100.7"},
		"proxy without headers":                 {"10.1.2.3", map[string]string{}, "10.1.2.3"},
		"untrusted peer forwarding":             {"203.0.113.5", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "203.0.113.5"},
		"untrusted peer near the trusted range": {"192.0.2.2", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "192.0.2.2"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := resolvedClientIP(t, app, tc.peer, tc.headers); got != tc.want {
				t.Fatalf("client IP = %q, want %q", got, tc.want)
			}
		})
	}
}

// Addresses and CIDRs of both families are accepted; anything else is
// reported and skipped, never trusted, and a report of several names them
// all.
func TestParseTrustedProxies(t *testing.T) {
	networks, err := ParseTrustedProxies([]string{" 10.0.0.0/8 ", "192.0.2.1", "2001:db8::1", "2001:db8::/32", ""})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}
	if len(networks) != 4 {
		t.Fatalf("networks = %v, want 4", networks)
	}
	for _, tc := range []struct {
		ip   string
		want bool
	}{{"10.200.1.1", true}, {"192.0.2.1", true}, {"192.0.2.2", false}, {"2001:db8::1", true}, {"2001:db8:1::5", true}, {"2001:db9::1", false}} {
		contained := false
		for _, network := range networks {
			if network.Contains(net.ParseIP(tc.ip)) {
				contained = true
			}
		}
		if contained != tc.want {
			t.Errorf("%s trusted = %v, want %v", tc.ip, contained, tc.want)
		}
	}

	networks, err = ParseTrustedProxies([]string{"10.0.0.0/8", "proxy.internal", "300.1.1.1"})
	if err == nil || len(networks) != 1 {
		t.Fatalf("ParseTrustedProxies() = %v, %v; want the one valid network and an error naming the rest", networks, err)
	}
	if got := err.Error(); !strings.Contains(got, "proxy.internal") || !strings.Contains(got, "300.1.1.1") {
		t.Fatalf("error = %q, want both invalid entries named", got)
	}
}

// The configured listener bounds reach Hertz; a zero bound leaves its
// default, so an unconfigured deployment keeps the timeouts and body limit
// it always had, the write timeout in particular unbounded.
func TestListenerOptionsFollowTheConfiguration(t *testing.T) {
	defaults := hertzconfig.NewOptions(nil)
	unconfigured := hertzconfig.NewOptions(listenerOptions(appconfig.HTTPConfig{}))
	if unconfigured.ReadTimeout != defaults.ReadTimeout || unconfigured.WriteTimeout != defaults.WriteTimeout ||
		unconfigured.IdleTimeout != defaults.IdleTimeout || unconfigured.MaxRequestBodySize != defaults.MaxRequestBodySize {
		t.Fatalf("unconfigured options = %+v, want Hertz's defaults", unconfigured)
	}

	configured := hertzconfig.NewOptions(listenerOptions(appconfig.HTTPConfig{
		ReadTimeoutSeconds: 30, WriteTimeoutSeconds: 45, IdleTimeoutSeconds: 60, MaxRequestBodyMB: 8,
	}))
	if configured.ReadTimeout != 30*time.Second || configured.WriteTimeout != 45*time.Second ||
		configured.IdleTimeout != 60*time.Second || configured.MaxRequestBodySize != 8<<20 {
		t.Fatalf("configured options = read %v write %v idle %v body %d", configured.ReadTimeout, configured.WriteTimeout, configured.IdleTimeout, configured.MaxRequestBodySize)
	}

	// The scaffolded defaults reproduce the values the server ran with.
	scaffold := hertzconfig.NewOptions(listenerOptions(appconfig.HTTPConfig{ReadTimeoutSeconds: 180, WriteTimeoutSeconds: 0, IdleTimeoutSeconds: 180, MaxRequestBodyMB: 4}))
	if scaffold.ReadTimeout != defaults.ReadTimeout || scaffold.WriteTimeout != defaults.WriteTimeout ||
		scaffold.IdleTimeout != defaults.IdleTimeout || scaffold.MaxRequestBodySize != defaults.MaxRequestBodySize {
		t.Fatalf("scaffolded options = read %v write %v idle %v body %d; defaults = read %v write %v idle %v body %d",
			scaffold.ReadTimeout, scaffold.WriteTimeout, scaffold.IdleTimeout, scaffold.MaxRequestBodySize,
			defaults.ReadTimeout, defaults.WriteTimeout, defaults.IdleTimeout, defaults.MaxRequestBodySize)
	}
}
