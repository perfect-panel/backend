package task

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The calendar reset is retried half an hour apart, the other tasks with
// asynq's backoff.
func TestRetryDelay(t *testing.T) {
	failure := errors.New("failed")
	for n := 1; n <= 3; n++ {
		if got := retryDelay(n, failure, asynq.NewTask(taskqueue.SchedulerResetTraffic, nil)); got != resetTrafficRetryDelay {
			t.Fatalf("reset retry %d delay = %v, want %v", n, got, resetTrafficRetryDelay)
		}
		if got := retryDelay(n, failure, asynq.NewTask(taskqueue.SchedulerFlushTraffic, nil)); got <= 0 || got >= resetTrafficRetryDelay {
			t.Fatalf("flush retry %d delay = %v, want asynq's backoff", n, got)
		}
	}
}

// A failed task's log line keeps the failure a coded error wraps: its Error
// text alone would lose the root cause.
func TestLogTaskFailureKeepsTheWrappedCause(t *testing.T) {
	var output bytes.Buffer
	oldWriter := logger.Reset()
	logger.SetWriter(logger.NewWriter(&output))
	t.Cleanup(func() {
		logger.Reset()
		if oldWriter != nil {
			logger.SetWriter(oldWriter)
		}
	})

	cause := errors.New("connection refused")
	logTaskFailure(context.Background(), asynq.NewTask(taskqueue.SchedulerTrafficStat, nil), xerr.Wrapf(cause, xerr.ERROR, "record the traffic stat"))
	if line := output.String(); !strings.Contains(line, "connection refused") || !strings.Contains(line, "record the traffic stat") {
		t.Fatalf("task failure log = %q, want the context and the cause", line)
	}
}

// fakeReadiness is the bootstrap's signal as the test drives it.
type fakeReadiness struct {
	done chan struct{}
	err  error
}

func newFakeReadiness() *fakeReadiness {
	return &fakeReadiness{done: make(chan struct{})}
}

func (r *fakeReadiness) Done() <-chan struct{} { return r.done }

func (r *fakeReadiness) Err() error {
	select {
	case <-r.done:
		return r.err
	default:
		return nil
	}
}

// fakeTaskServer records when the worker started consuming and how often
// it was shut down.
type fakeTaskServer struct {
	started chan struct{}

	mu        sync.Mutex
	shutdowns int
}

func newFakeTaskServer() *fakeTaskServer {
	return &fakeTaskServer{started: make(chan struct{})}
}

func (s *fakeTaskServer) Start(asynq.Handler) error {
	close(s.started)
	return nil
}

func (s *fakeTaskServer) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns++
}

func (s *fakeTaskServer) shutdownCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdowns
}

// startInBackground runs Start and reports when it returned.
func startInBackground(svc *Service) <-chan struct{} {
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		svc.Start()
	}()
	return returned
}

func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen", what)
	}
}

// The HTTP service's bootstrap loads the settings the handlers read while
// the worker starts: the worker must not consume before the bootstrap
// signals, and consumes once it does. Stop shuts the server down and ends
// Start.
func TestStartConsumesOnceTheRuntimeSettingsAreLoaded(t *testing.T) {
	logtest.Discard(t)
	bootstrapped := newFakeReadiness()
	server := newFakeTaskServer()
	svc := newService(server, Dependencies{Bootstrapped: bootstrapped})
	returned := startInBackground(svc)

	select {
	case <-server.started:
		t.Fatal("the worker consumed before the runtime settings were loaded")
	case <-time.After(100 * time.Millisecond):
	}
	close(bootstrapped.done)
	awaitClosed(t, server.started, "consuming after the bootstrap")

	svc.Stop()
	awaitClosed(t, returned, "Start returning after Stop")
	if server.shutdownCount() != 1 {
		t.Fatalf("server shut down %d times, want once", server.shutdownCount())
	}
}

// A failed bootstrap takes the process down; the worker does not start
// consuming on empty settings in the meantime and says why.
func TestStartDoesNotConsumeAfterAFailedBootstrap(t *testing.T) {
	logs := logtest.NewCollector(t)
	bootstrapped := newFakeReadiness()
	bootstrapped.err = xerr.Wrapf(errors.New("connection refused"), xerr.ERROR, "migrate the database")
	close(bootstrapped.done)
	server := newFakeTaskServer()
	svc := newService(server, Dependencies{Bootstrapped: bootstrapped})

	svc.Start()

	select {
	case <-server.started:
		t.Fatal("the worker consumed after a failed bootstrap")
	default:
	}
	if out := logs.String(); !strings.Contains(out, "runtime bootstrap failed") || !strings.Contains(out, "connection refused") {
		t.Fatalf("log = %s, want the failed bootstrap reported with its cause", out)
	}
	svc.Stop()
}

// A bootstrap signal that never comes must not leave the queue unserved for
// good: after the bound the worker consumes and reports the wait.
func TestStartConsumesAfterTheBootstrapWaitExpires(t *testing.T) {
	logs := logtest.NewCollector(t)
	server := newFakeTaskServer()
	svc := newService(server, Dependencies{Bootstrapped: newFakeReadiness()})
	svc.bootstrapTimeout = 20 * time.Millisecond
	returned := startInBackground(svc)

	awaitClosed(t, server.started, "consuming after the bootstrap wait expired")
	if out := logs.String(); !strings.Contains(out, "not loaded in time") {
		t.Fatalf("log = %s, want the expired wait reported", out)
	}
	svc.Stop()
	awaitClosed(t, returned, "Start returning after Stop")
}

// Stopping a worker that still waits for the bootstrap ends the wait without
// consuming.
func TestStopEndsTheBootstrapWait(t *testing.T) {
	logtest.Discard(t)
	server := newFakeTaskServer()
	svc := newService(server, Dependencies{Bootstrapped: newFakeReadiness()})
	returned := startInBackground(svc)

	svc.Stop()

	awaitClosed(t, returned, "Start returning after Stop")
	select {
	case <-server.started:
		t.Fatal("the worker consumed after Stop")
	default:
	}
	// Stop is idempotent and Start after Stop consumes nothing.
	svc.Stop()
	svc.Start()
	if server.shutdownCount() != 1 {
		t.Fatalf("server shut down %d times, want once", server.shutdownCount())
	}
}
