package cmd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/config"
)

// healthConfig writes a configuration file naming the listener at addr and
// returns its path.
func healthConfig(t *testing.T, addr string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ppanel.yaml")
	if err := os.WriteFile(path, []byte("Host: "+host+"\nPort: "+port+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The healthcheck command exits zero only when the server the configuration
// file describes answers /healthz with 200 within the timeout.
func TestHealthcheckAsksTheConfiguredServer(t *testing.T) {
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	path := healthConfig(t, strings.TrimPrefix(server.URL, "http://"))

	status = http.StatusOK
	if err := healthcheck(context.Background(), path, time.Second); err != nil {
		t.Fatalf("healthcheck against a healthy server = %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := healthcheck(context.Background(), path, time.Second); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("healthcheck against a 503 = %v, want the status reported", err)
	}

	server.Close()
	if err := healthcheck(context.Background(), path, time.Second); err == nil {
		t.Fatal("healthcheck against a closed server returned nil")
	}
	if err := healthcheck(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"), time.Second); err == nil {
		t.Fatal("healthcheck without a configuration file returned nil")
	}
}

// A server that binds every interface is reached on the loopback address;
// one bound to an address is reached there. TLS selects https.
func TestHealthURLFollowsTheListener(t *testing.T) {
	for _, tc := range []struct {
		host string
		tls  bool
		want string
	}{
		{"0.0.0.0", false, "http://127.0.0.1:8080/healthz"},
		{"", false, "http://127.0.0.1:8080/healthz"},
		{"::", false, "http://127.0.0.1:8080/healthz"},
		{"192.168.1.5", false, "http://192.168.1.5:8080/healthz"},
		{"::1", true, "https://[::1]:8080/healthz"},
	} {
		c := config.File{Host: tc.host, Port: 8080}
		c.TLS.Enable = tc.tls
		if got := healthURL(c); got != tc.want {
			t.Errorf("healthURL(host %q, tls %t) = %q, want %q", tc.host, tc.tls, got, tc.want)
		}
	}
}

// The healthcheck times out on a server that accepts and never answers.
func TestHealthcheckGivesUpAfterTheTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	path := healthConfig(t, "127.0.0.1:"+port)

	start := time.Now()
	err = healthcheck(context.Background(), path, 200*time.Millisecond)

	if err == nil {
		t.Fatal("healthcheck against a silent server returned nil")
	}
	if waited := time.Since(start); waited > 3*time.Second {
		t.Fatalf("healthcheck waited %s, want the timeout honoured", waited)
	}
}
