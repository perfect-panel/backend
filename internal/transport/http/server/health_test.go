package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// A listener that cannot bind used to be logged and forgotten, leaving a
// process without an API; Start reports it to the caller now.
func TestStartReportsAListenerItCannotBind(t *testing.T) {
	logtest.Discard(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()

	err = New(Dependencies{}, occupied.Addr().String(), nil).Start()

	if err == nil {
		t.Fatal("Start returned nil although the port is taken")
	}
	if !strings.Contains(err.Error(), "bind") && !strings.Contains(err.Error(), "in use") {
		t.Fatalf("Start error = %v, want the bind failure", err)
	}
}

// A shutdown asked for is not a failure: Start returns nil once Shutdown
// closed the listener.
func TestStartReturnsNilAfterShutdown(t *testing.T) {
	logtest.Discard(t)
	srv := New(Dependencies{}, "127.0.0.1:0", nil)
	result := make(chan error, 1)
	go func() { result <- srv.Start() }()

	deadline := time.Now().Add(5 * time.Second)
	for !srv.Engine().IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Start returned %v after Shutdown, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// readiness is a bootstrap signal under the test's control.
type readiness struct {
	done chan struct{}
	err  error
}

func newReadiness() *readiness { return &readiness{done: make(chan struct{})} }

func (r *readiness) Done() <-chan struct{} { return r.done }
func (r *readiness) Err() error            { return r.err }
func (r *readiness) finish(err error) {
	r.err = err
	close(r.done)
}

type healthResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func probeHealth(t *testing.T, srv *Server, path string) (int, healthResponse) {
	t.Helper()
	status, body := performNativeRequest(srv, http.MethodGet, path)
	var response healthResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("%s answered %q: %v", path, body, err)
	}
	return status, response
}

// /healthz is liveness: it answers 200 as soon as the listener serves.
// /readyz is readiness: 503 with the reason until the bootstrap fired and
// every dependency answers, 200 afterwards.
func TestHealthEndpoints(t *testing.T) {
	logtest.Discard(t)
	bootstrapped := newReadiness()
	var pings atomic.Int32
	var redisDown atomic.Bool
	srv := New(Dependencies{}, "127.0.0.1:0", nil)
	RegisterHealthHandlers(srv.Engine(), HealthDependencies{
		Bootstrapped: bootstrapped,
		Probes: []Probe{
			{Name: "database", Ping: func(context.Context) error { pings.Add(1); return nil }},
			{Name: "redis", Ping: func(context.Context) error {
				if redisDown.Load() {
					return errors.New("dial tcp 10.0.0.9:6379: connection refused")
				}
				return nil
			}},
		},
		ProbeTTL: time.Hour,
	})

	if status, response := probeHealth(t, srv, HealthzPath); status != http.StatusOK || response.Status != "ok" {
		t.Fatalf("/healthz = %d %+v before the bootstrap, want 200 ok", status, response)
	}
	if status, response := probeHealth(t, srv, ReadyzPath); status != http.StatusServiceUnavailable || !strings.Contains(response.Reason, "not finished") {
		t.Fatalf("/readyz = %d %+v before the bootstrap, want 503 with the bootstrap pending", status, response)
	}
	if pings.Load() != 0 {
		t.Fatal("the dependencies were pinged before the bootstrap finished")
	}

	bootstrapped.finish(nil)
	if status, response := probeHealth(t, srv, ReadyzPath); status != http.StatusOK || response.Status != "ok" {
		t.Fatalf("/readyz = %d %+v after the bootstrap, want 200 ok", status, response)
	}
	// The probe result is cached: a burst of checks pings once.
	probeHealth(t, srv, ReadyzPath)
	probeHealth(t, srv, ReadyzPath)
	if pings.Load() != 1 {
		t.Fatalf("the database was pinged %d times within the cache lifetime, want once", pings.Load())
	}
}

// A dependency that stops answering makes the server not ready, and the
// response names the dependency without its address.
func TestReadyzReportsTheDependencyThatDoesNotAnswer(t *testing.T) {
	logs := logtest.NewCollector(t)
	bootstrapped := newReadiness()
	bootstrapped.finish(nil)
	srv := New(Dependencies{}, "127.0.0.1:0", nil)
	RegisterHealthHandlers(srv.Engine(), HealthDependencies{
		Bootstrapped: bootstrapped,
		Probes: []Probe{{Name: "redis", Ping: func(context.Context) error {
			return errors.New("dial tcp 10.0.0.9:6379: connection refused")
		}}},
		ProbeTTL: time.Nanosecond,
	})

	status, response := probeHealth(t, srv, ReadyzPath)

	if status != http.StatusServiceUnavailable || response.Reason != "redis unreachable" {
		t.Fatalf("/readyz = %d %+v, want 503 naming redis", status, response)
	}
	if strings.Contains(response.Reason, "10.0.0.9") {
		t.Fatalf("the response leaks the address: %+v", response)
	}
	if !strings.Contains(logs.String(), "10.0.0.9") {
		t.Fatalf("log = %s, want the ping failure with its detail", logs.String())
	}
	// A failed bootstrap is reported as such, and never pinged past.
	failed := newReadiness()
	failed.finish(errors.New("migration failed"))
	srv = New(Dependencies{}, "127.0.0.1:0", nil)
	RegisterHealthHandlers(srv.Engine(), HealthDependencies{Bootstrapped: failed})
	if status, response := probeHealth(t, srv, ReadyzPath); status != http.StatusServiceUnavailable || !strings.Contains(response.Reason, "failed") {
		t.Fatalf("/readyz = %d %+v after a failed bootstrap, want 503 failed", status, response)
	}
}

// The probe cache expires: after the lifetime the dependencies are pinged
// again and a recovered dependency makes the server ready.
func TestReadyzProbeCacheExpires(t *testing.T) {
	logtest.Discard(t)
	bootstrapped := newReadiness()
	bootstrapped.finish(nil)
	var down atomic.Bool
	down.Store(true)
	deps := HealthDependencies{
		Bootstrapped: bootstrapped,
		Probes: []Probe{{Name: "database", Ping: func(context.Context) error {
			if down.Load() {
				return errors.New("down")
			}
			return nil
		}}},
		ProbeTTL: time.Minute,
	}
	checker := newHealthChecker(deps)
	now := time.Now()
	checker.now = func() time.Time { return now }

	if reason := checker.notReady(context.Background()); reason != "database unreachable" {
		t.Fatalf("reason = %q, want database unreachable", reason)
	}
	down.Store(false)
	if reason := checker.notReady(context.Background()); reason != "database unreachable" {
		t.Fatalf("reason = %q within the cache lifetime, want the cached failure", reason)
	}
	now = now.Add(2 * time.Minute)
	if reason := checker.notReady(context.Background()); reason != "" {
		t.Fatalf("reason = %q after the cache expired, want ready", reason)
	}
}
