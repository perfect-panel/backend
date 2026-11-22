package httpserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Health endpoint paths, for the orchestrator's probes and the healthcheck
// command.
const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
)

// Readiness is the runtime bootstrap's signal: Done closes once the runtime
// settings are loaded or the bootstrap failed, and Err reports the failure.
// The composition root's lifecycle.Readiness provides it.
type Readiness interface {
	Done() <-chan struct{}
	Err() error
}

// Probe pings one dependency the server cannot serve without, such as the
// database or Redis. Name is what the readiness response reports when the
// ping fails; the error itself is only logged, so no address reaches a
// client.
type Probe struct {
	Name string
	Ping func(ctx context.Context) error
}

// HealthDependencies is what the health endpoints report on.
type HealthDependencies struct {
	Bootstrapped Readiness
	Probes       []Probe
	// ProbeTTL is how long a probe result is reused, so readiness checks
	// cannot hammer the database and Redis; zero means DefaultProbeTTL.
	ProbeTTL time.Duration
	// ProbeTimeout bounds one round of pings; zero means
	// DefaultProbeTimeout.
	ProbeTimeout time.Duration
}

// DefaultProbeTTL and DefaultProbeTimeout are the probe cache lifetime and
// the bound on one round of pings when HealthDependencies names none.
const (
	DefaultProbeTTL     = 5 * time.Second
	DefaultProbeTimeout = 2 * time.Second
)

// RegisterHealthHandlers adds the liveness and readiness endpoints to
// engine. They carry no authentication and reveal nothing but a status and,
// when not ready, the name of the dependency that does not answer.
func RegisterHealthHandlers(engine *server.Hertz, deps HealthDependencies) {
	checker := newHealthChecker(deps)
	engine.GET(HealthzPath, healthzHandler())
	engine.GET(ReadyzPath, readyzHandler(checker))
}

// healthzHandler answers the liveness probe.
//
// @Summary Liveness probe
// @Description Reports that the process serves HTTP. It answers 200 whenever the listener is up, regardless of the database and Redis; use /readyz for those.
// @Tags common
// @Produce json
// @Success 200 {object} map[string]string "status ok"
// @Router /healthz [get]
func healthzHandler() app.HandlerFunc {
	return func(_ context.Context, ctx *app.RequestContext) {
		ctx.JSON(http.StatusOK, utils.H{"status": "ok"})
	}
}

// readyzHandler answers the readiness probe.
//
// @Summary Readiness probe
// @Description Reports whether the server can serve requests: the runtime bootstrap has loaded the settings and the database and Redis answer a ping (the ping result is cached for a few seconds). Otherwise it answers 503 with the reason.
// @Tags common
// @Produce json
// @Success 200 {object} map[string]string "status ok"
// @Failure 503 {object} map[string]string "status unavailable with a reason"
// @Router /readyz [get]
func readyzHandler(checker *healthChecker) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		if reason := checker.notReady(c); reason != "" {
			ctx.JSON(http.StatusServiceUnavailable, utils.H{"status": "unavailable", "reason": reason})
			return
		}
		ctx.JSON(http.StatusOK, utils.H{"status": "ok"})
	}
}

// healthChecker evaluates readiness, reusing a probe result for ttl.
type healthChecker struct {
	deps    HealthDependencies
	ttl     time.Duration
	timeout time.Duration

	mu        sync.Mutex
	checkedAt time.Time
	reason    string
	now       func() time.Time
}

func newHealthChecker(deps HealthDependencies) *healthChecker {
	checker := &healthChecker{deps: deps, ttl: deps.ProbeTTL, timeout: deps.ProbeTimeout, now: time.Now}
	if checker.ttl <= 0 {
		checker.ttl = DefaultProbeTTL
	}
	if checker.timeout <= 0 {
		checker.timeout = DefaultProbeTimeout
	}
	return checker
}

// notReady returns why the server is not ready, or "" when it is.
func (h *healthChecker) notReady(ctx context.Context) string {
	if h.deps.Bootstrapped != nil {
		select {
		case <-h.deps.Bootstrapped.Done():
			if err := h.deps.Bootstrapped.Err(); err != nil {
				return "runtime bootstrap failed"
			}
		default:
			return "runtime bootstrap not finished"
		}
	}
	return h.probe(ctx)
}

// probe pings the dependencies, or reuses the last result while it is
// fresh. The lock serialises concurrent probes, so a burst of readiness
// checks costs one round of pings.
func (h *healthChecker) probe(ctx context.Context) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checkedAt.IsZero() && h.now().Sub(h.checkedAt) < h.ttl {
		return h.reason
	}
	h.reason = h.ping(ctx)
	h.checkedAt = h.now()
	return h.reason
}

func (h *healthChecker) ping(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	for _, probe := range h.deps.Probes {
		if probe.Ping == nil {
			continue
		}
		if err := probe.Ping(ctx); err != nil {
			logger.Errorw("[Health] dependency does not answer", logger.Field("dependency", probe.Name), logger.Field("error", err.Error()))
			return probe.Name + " unreachable"
		}
	}
	return ""
}
