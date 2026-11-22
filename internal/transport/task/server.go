// Package task is the application's asynq consumer: it registers the handler
// of every task type and runs them behind the trace middleware, with one
// failure log line per attempt and the retry policy of the scheduled tasks.
// The handlers live in the subpackages and only decode payloads and call the
// modules, which own the transactions.
package task

import (
	"context"
	"sync"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// resetTrafficRetryDelay spaces the calendar traffic reset's retries so a
// database or Redis outage can clear in between; every retry stays on the
// reset's day, which its once-per-day guard is keyed by.
const resetTrafficRetryDelay = 30 * time.Minute

// bootstrapTimeout bounds the wait for the runtime settings. A bootstrap
// that takes longer is stuck, not slow: the worker then consumes anyway and
// says so, rather than leaving the queue unserved for good.
const bootstrapTimeout = 10 * time.Minute

// Readiness is the signal that the runtime settings the handlers read — the
// email and SMS platforms, the site name, the Telegram bot — are loaded:
// Done closes once the bootstrap finished, and Err reports its failure.
type Readiness interface {
	Done() <-chan struct{}
	Err() error
}

// Service is the task worker the process runs next to the HTTP server and
// the scheduler.
type Service struct {
	deps             Dependencies
	server           taskServer
	bootstrapTimeout time.Duration

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

// taskServer is the asynq server the worker consumes with.
type taskServer interface {
	Start(asynq.Handler) error
	Shutdown()
}

// NewService builds the task consumer on the queue's Redis connection; the
// composition root owns that connection's settings.
func NewService(redisOpt asynq.RedisConnOpt, deps Dependencies) *Service {
	return newService(initService(redisOpt), deps)
}

func newService(server taskServer, deps Dependencies) *Service {
	return &Service{
		deps:             deps,
		server:           server,
		bootstrapTimeout: bootstrapTimeout,
		done:             make(chan struct{}),
	}
}

// Start registers the handlers and consumes tasks until Stop. Consuming
// begins once the runtime settings the handlers read are loaded: a task run
// before that, such as an email queued during the start, would read empty
// settings and be dropped. The lifecycle group starts the services together,
// so the wait for the HTTP service's bootstrap happens here.
func (m *Service) Start() {
	mux := asynq.NewServeMux()
	// Resume the producer's trace from the payload envelope and span every
	// task execution before any handler runs.
	mux.Use(taskqueue.Middleware())
	// register tasks
	RegisterHandlers(mux, m.deps)

	if !m.awaitRuntimeSettings() {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	logger.Infof("start consumer service")
	err := m.server.Start(mux)
	m.mu.Unlock()
	if err != nil {
		logger.Error("consumer service error", logger.LogField{
			Key:   "error",
			Value: err.Error(),
		})
		return
	}
	<-m.done
}

// awaitRuntimeSettings waits for the bootstrap and reports whether the
// worker consumes: not after a failed bootstrap, which takes the process
// down, and not once stopped. A bootstrap that has not finished within the
// bound is reported, and the worker consumes anyway.
func (m *Service) awaitRuntimeSettings() bool {
	if m.deps.Bootstrapped == nil {
		return true
	}
	timeout := time.NewTimer(m.bootstrapTimeout)
	defer timeout.Stop()
	select {
	case <-m.deps.Bootstrapped.Done():
		if err := m.deps.Bootstrapped.Err(); err != nil {
			logger.Errorw("[Task] runtime bootstrap failed, not consuming tasks", logger.Field("error", xerr.Detail(err)))
			return false
		}
		return true
	case <-timeout.C:
		logger.Errorw("[Task] runtime settings not loaded in time, consuming tasks anyway", logger.Field("waited", m.bootstrapTimeout))
		return true
	case <-m.done:
		return false
	}
}

// Stop stops pulling tasks, waits for the running handlers (up to asynq's
// shutdown timeout, eight seconds, after which an unfinished task goes back
// to its queue) and closes the queue connection. Start returns once it is
// done.
func (m *Service) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	logger.Info("stop consumer service")
	// A no-op on a server that never started.
	m.server.Shutdown()
	close(m.done)
}

func initService(redisOpt asynq.RedisConnOpt) *asynq.Server {
	return asynq.NewServer(
		redisOpt,
		asynq.Config{
			// The one log line of a failed attempt, naming the task and its
			// retry budget; handlers return their errors without logging them
			// a second time.
			ErrorHandler:   asynq.ErrorHandlerFunc(logTaskFailure),
			RetryDelayFunc: retryDelay,
			Concurrency:    20,
		},
	)
}

// logTaskFailure logs a failed task run once, with the whole error chain: a
// coded error's message leaves out the failure it wraps, so the log line
// uses xerr.Detail rather than Error.
func logTaskFailure(ctx context.Context, task *asynq.Task, err error) {
	id, _ := asynq.GetTaskID(ctx)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	logger.WithContext(ctx).Errorw("[Task] task failed",
		logger.Field("task_type", task.Type()),
		logger.Field("task_id", id),
		logger.Field("retried", retried),
		logger.Field("max_retry", maxRetry),
		logger.Field("error", xerr.Detail(err)),
	)
}

// retryDelay spaces the retries of a failed task: asynq's backoff, except for
// the calendar traffic reset (see resetTrafficRetryDelay).
func retryDelay(n int, err error, task *asynq.Task) time.Duration {
	if task.Type() == taskqueue.SchedulerResetTraffic {
		return resetTrafficRetryDelay
	}
	return asynq.DefaultRetryDelayFunc(n, err, task)
}
