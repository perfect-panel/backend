package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	httpserver "github.com/perfect-panel/backend/internal/transport/http/server"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// fakeServer is a transport server under the test's control: Start blocks
// until Shutdown, or returns startErr at once.
type fakeServer struct {
	startErr  error
	started   chan struct{}
	stop      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	shutdowns atomic.Int32
}

func newFakeServer(startErr error) *fakeServer {
	return &fakeServer{startErr: startErr, started: make(chan struct{}), stop: make(chan struct{})}
}

func (f *fakeServer) Start() error {
	f.startOnce.Do(func() { close(f.started) })
	if f.startErr != nil {
		return f.startErr
	}
	<-f.stop
	return nil
}

func (f *fakeServer) Shutdown(context.Context) error {
	f.shutdowns.Add(1)
	f.stopOnce.Do(func() { close(f.stop) })
	return nil
}

// A server that stops with an error, a listener that could not bind above
// all, ends the process like a certificate that does not load; it used to be
// logged only, leaving a process that consumed tasks with no API.
func TestServeFailsFastWhenTheServerCannotListen(t *testing.T) {
	logs := logtest.NewCollector(t)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		serve(newFakeServer(errors.New("listen tcp :8080: bind: address already in use")))
	}()
	if recovered == nil {
		t.Fatal("serve returned although the server could not listen")
	}
	if !strings.Contains(logs.String(), "address already in use") {
		t.Fatalf("log = %s, want the listen failure", logs.String())
	}

	// A server that stopped because it was shut down ends serve quietly.
	server := newFakeServer(nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(server)
	}()
	<-server.started
	_ = server.Shutdown(context.Background())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after Shutdown")
	}
}

// restartable builds a service whose server and reload are fakes: current
// is the running server, next what the restart builds.
func restartable(t *testing.T, current *fakeServer, next func() transportServer, reloadErr error) (*Service, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var c config.Config
	c.Host, c.Port = "127.0.0.1", 18080
	var reloads, builds atomic.Int32
	svc := NewService(Dependencies{
		Config: func() config.Config { return c },
		HTTP:   func() httpserver.Dependencies { return httpserver.Dependencies{} },
	})
	svc.reload = func(context.Context) error {
		reloads.Add(1)
		return reloadErr
	}
	svc.newServer = func(_ httpserver.Dependencies, _ config.Config, addr string, _ httpserver.HealthDependencies) transportServer {
		builds.Add(1)
		if addr != "127.0.0.1:18080" {
			t.Errorf("restart binds %s, want the configured 127.0.0.1:18080", addr)
		}
		return next()
	}
	svc.server = current
	return svc, &reloads, &builds
}

// Restart reloads the runtime settings, shuts the running server down and
// serves a new one; the bootstrap (migration, seeding) does not run again.
func TestRestartReloadsSettingsAndRebindsTheServer(t *testing.T) {
	logtest.Discard(t)
	current, next := newFakeServer(nil), newFakeServer(nil)
	svc, reloads, builds := restartable(t, current, func() transportServer { return next }, nil)

	if err := svc.Restart(); err != nil {
		t.Fatalf("Restart() = %v", err)
	}

	if reloads.Load() != 1 || builds.Load() != 1 {
		t.Fatalf("reloads = %d, builds = %d, want one each", reloads.Load(), builds.Load())
	}
	if current.shutdowns.Load() != 1 {
		t.Fatalf("the running server was shut down %d times, want once", current.shutdowns.Load())
	}
	select {
	case <-next.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the new server was not started")
	}
	if svc.server != next {
		t.Fatal("the service does not hold the new server")
	}
	// Stop reaches the new server.
	svc.Stop()
	if next.shutdowns.Load() != 1 {
		t.Fatalf("Stop shut the new server down %d times, want once", next.shutdowns.Load())
	}
	// No restart after Stop.
	if err := svc.Restart(); err == nil {
		t.Fatal("Restart after Stop returned nil")
	}
}

// A reload that fails keeps the previous settings and the running server,
// and the error reaches the administrator.
func TestRestartReportsAFailedReloadAndKeepsTheServer(t *testing.T) {
	logtest.Discard(t)
	current := newFakeServer(nil)
	cause := errors.New("database down")
	svc, _, builds := restartable(t, current, func() transportServer { t.Fatal("a server was built"); return nil }, cause)

	err := svc.Restart()

	if !errors.Is(err, cause) {
		t.Fatalf("Restart() = %v, want the reload failure", err)
	}
	if builds.Load() != 0 || current.shutdowns.Load() != 0 || svc.server != current {
		t.Fatal("a failed reload replaced or shut down the running server")
	}
}

// A new server that cannot listen is reported instead of ending the
// process, and the failure is logged.
func TestRestartReportsAServerThatCannotListen(t *testing.T) {
	logs := logtest.NewCollector(t)
	current := newFakeServer(nil)
	svc, _, _ := restartable(t, current, func() transportServer {
		return newFakeServer(errors.New("listen tcp 127.0.0.1:18080: bind: address already in use"))
	}, nil)

	err := svc.Restart()

	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Restart() = %v, want the listen failure", err)
	}
	if !strings.Contains(logs.String(), "restarted http server failed") {
		t.Fatalf("log = %s, want the failed restart", logs.String())
	}
}

// A server builder that panics — the certificate does not load — is
// recovered: the error is reported and the running server keeps serving.
func TestRestartRecoversFromAPanickingServerBuilder(t *testing.T) {
	logtest.Discard(t)
	current := newFakeServer(nil)
	svc, _, _ := restartable(t, current, func() transportServer { panic("load tls certificate: no such file") }, nil)

	err := svc.Restart()

	if err == nil || !strings.Contains(err.Error(), "load tls certificate") {
		t.Fatalf("Restart() = %v, want the recovered panic", err)
	}
	if current.shutdowns.Load() != 0 || svc.server != current {
		t.Fatal("the running server was replaced although the new one could not be built")
	}
}
