package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/perfect-panel/backend/internal/app/bootstrap"
	"github.com/perfect-panel/backend/internal/app/lifecycle"
	"github.com/perfect-panel/backend/internal/config"
	httpserver "github.com/perfect-panel/backend/internal/transport/http/server"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/trace"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// shutdownTimeout is how long open requests get when the server stops or
// restarts. It counts towards the process's stop budget; see services.
const shutdownTimeout = 5 * time.Second

// restartGrace is how long Restart waits for the new server to fail before
// reporting success: a listener that cannot bind fails within milliseconds,
// and the administrator who asked for the restart should learn of it.
const restartGrace = 500 * time.Millisecond

// Service is the HTTP service: it runs the bootstrap, then serves the routes,
// and restarts the server when an administrator changes the subscribe path.
type Service struct {
	deps Dependencies
	// newServer builds the transport server on the routes; tests replace it.
	newServer func(deps httpserver.Dependencies, runtimeConfig config.Config, addr string, health httpserver.HealthDependencies) transportServer
	// reload re-reads the runtime settings before Restart rebuilds the
	// routes on them; tests replace it.
	reload func(ctx context.Context) error

	// mu guards server and stopped: Stop and Restart replace or close the
	// server, and a restart must not follow a stop.
	mu      sync.Mutex
	server  transportServer
	stopped bool
}

// Dependencies is what the HTTP service needs to start and restart: the
// runtime configuration and the bootstrap that loads it, the identity
// module's startup work, the routes and the runtime hooks it installs.
type Dependencies struct {
	Config                 func() config.Config
	Bootstrap              *bootstrap.Dependencies
	HTTP                   func() httpserver.Dependencies
	SetRestart             func(func() error)
	SetReinitializeHandler func(func(string) error)
	// Identity runs the identity module's startup check and data fix-up
	// once the bootstrap migrated the schema; the identity facade provides
	// it.
	Identity IdentityStartup
	// Bootstrapped tells the services that read the runtime settings, the
	// task worker among them, that the bootstrap published them or failed.
	// The readiness endpoint reports it too.
	Bootstrapped *lifecycle.Readiness
	// Probes are the dependencies the readiness endpoint pings: the
	// database and Redis.
	Probes []httpserver.Probe
}

// IdentityStartup is the identity module's part of the server start: the
// check that refuses stored email bindings email sign-in cannot tell apart,
// and the fix-up that stores phone numbers in E.164.
type IdentityStartup interface {
	ValidateEmailIdentities(ctx context.Context) error
	NormalizePhoneNumbers(ctx context.Context) error
}

// NewService builds the HTTP service; Start runs the bootstrap.
func NewService(deps Dependencies) *Service {
	return &Service{
		deps:      deps,
		newServer: newTransportServer,
		reload: func(ctx context.Context) error {
			return bootstrap.ReloadAll(ctx, deps.Bootstrap)
		},
	}
}

// transportServer is the HTTP server the service runs: Start serves until
// Shutdown and returns the error that stopped it before then.
type transportServer interface {
	Start() error
	Shutdown(ctx context.Context) error
}

// newTransportServer builds the server on the routes, with the health
// endpoints, and loads the TLS certificate when TLS is enabled. It panics
// when the certificate does not load: a process that keeps running without
// listening hides the outage from the orchestrator.
func newTransportServer(deps httpserver.Dependencies, runtimeConfig config.Config, addr string, health httpserver.HealthDependencies) transportServer {
	var tlsConfig *tls.Config
	if runtimeConfig.TLS.Enable {
		cert, err := tls.LoadX509KeyPair(runtimeConfig.TLS.CertFile, runtimeConfig.TLS.KeyFile)
		if err != nil {
			logger.Errorf("load tls certificate error: %s", err.Error())
			panic(fmt.Sprintf("load tls certificate: %v", err))
		}
		tlsConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
	}
	server := httpserver.New(deps, addr, tlsConfig)
	httpserver.RegisterHealthHandlers(server.Engine(), health)
	return server
}

// Start loads the runtime configuration, installs the runtime hooks and
// serves until Stop or Restart. It ends the process when the bootstrap fails
// or the server cannot listen: a process without an API that still consumed
// tasks would look healthy to the orchestrator.
func (m *Service) Start() {
	if m.deps.Config == nil || m.deps.Bootstrap == nil || m.deps.HTTP == nil {
		panic("the HTTP service is missing its configuration, bootstrap or routes")
	}

	// The start-up work belongs to no request.
	ctx := context.Background()
	runtimeConfig := m.deps.Config()
	serverAddr := listenAddress(runtimeConfig)
	if err := bootstrap.Start(ctx, m.deps.Bootstrap); err != nil {
		// Fail fast: serving with a partially loaded configuration would
		// silently run with defaults such as open registration. Detail keeps
		// the database failure behind a coded error, such as the first
		// administrator's, in the line. The services waiting for the
		// settings learn of the failure before the process goes down.
		logger.Errorf("bootstrap error: %s", xerr.Detail(err))
		m.deps.Bootstrapped.Fail(err)
		panic(err)
	}
	m.deps.Bootstrapped.Ready()
	if err := m.deps.Identity.ValidateEmailIdentities(ctx); err != nil {
		logger.Errorf("stored email identities: %s", xerr.Detail(err))
		panic(err)
	}
	normalizeIdentityData(ctx, m.deps.Identity)
	server := m.newServer(m.deps.HTTP(), m.deps.Config(), serverAddr, m.health())
	m.mu.Lock()
	m.server = server
	m.mu.Unlock()
	traceConfig := runtimeConfig.Trace
	if traceConfig.Name == "" {
		traceConfig.Name = trace.TraceName
	}
	trace.StartAgent(traceConfig)
	if m.deps.SetRestart != nil {
		m.deps.SetRestart(m.Restart)
	}
	// A failed reload keeps the previous configuration and is reported to
	// the administrator who changed the settings. The hook carries no
	// context (the platform and identity modules call it after saving the
	// settings), so the reload runs on a root context.
	reinitialize := func(subsystem string) error {
		return bootstrap.Reload(context.Background(), m.deps.Bootstrap, bootstrap.Subsystem(subsystem))
	}
	if m.deps.SetReinitializeHandler != nil {
		m.deps.SetReinitializeHandler(reinitialize)
	}
	logger.Infof("server start at %v", serverAddr)
	serve(server)
}

// listenAddress is the address the configuration binds the API to.
func listenAddress(c config.Config) string {
	return fmt.Sprintf("%v:%d", c.Host, c.Port)
}

// health is what the health endpoints report on: the bootstrap signal and
// the dependency probes.
func (m *Service) health() httpserver.HealthDependencies {
	health := httpserver.HealthDependencies{Probes: m.deps.Probes}
	if m.deps.Bootstrapped != nil {
		health.Bootstrapped = m.deps.Bootstrapped
	}
	return health
}

// serve runs server until it is shut down. A server that stops with an
// error — one that could not bind its listener above all — ends the process,
// the way a certificate that does not load does: the error used to be logged
// only, and the process went on consuming tasks with no API, invisible to
// the orchestrator.
func serve(server transportServer) {
	if err := server.Start(); err != nil {
		logger.Errorf("http server error: %s", err.Error())
		panic(fmt.Sprintf("http server: %v", err))
	}
}

// Stop shuts the server down, giving open requests shutdownTimeout. The
// trace exporter is flushed by the service group once every service
// stopped.
func (m *Service) Stop() {
	m.mu.Lock()
	m.stopped = true
	server := m.server
	m.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Errorf("server shutdown error: %s", err.Error())
	}
	logger.Info("server shutdown")
}

// Restart applies the runtime settings the routes are built from — the
// subscribe path — and serves them from a new server: it reloads the
// settings, builds the new server, shuts the old one down and starts the
// new one. An administrator's request triggers it, and that request is
// served by the server being shut down, so the shutdown runs on its own
// context. It does not re-run the bootstrap (migration and seeding are
// startup work), and it reports a failure instead of ending the process: a
// failed reload keeps the previous settings and the running server, a
// certificate that does not load keeps the running server, and a new server
// that cannot listen is reported to the caller and logged.
func (m *Service) Restart() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("restart the http server: %v", r)
			logger.Errorf("[Restart] %s", err.Error())
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return errors.New("the server is stopping")
	}
	if m.server == nil {
		return errors.New("server is nil")
	}
	// The settings first: a reload that fails leaves the routes as they are.
	if err := m.reload(context.Background()); err != nil {
		return err
	}
	runtimeConfig := m.deps.Config()
	// The new server is built before the old one stops, so a certificate
	// that does not load costs nothing.
	server := m.newServer(m.deps.HTTP(), runtimeConfig, listenAddress(runtimeConfig), m.health())

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := m.server.Shutdown(ctx); err != nil {
		logger.Errorf("server shutdown error: %v", err.Error())
		return err
	}
	logger.Info("server shutdown")
	m.server = server

	failed := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// The process stays up without an API; the health endpoint
				// is what shows the orchestrator the outage.
				logger.Errorf("[Restart] the restarted http server failed: %v", r)
				failed <- fmt.Errorf("%v", r)
			}
		}()
		serve(server)
	}()
	select {
	case err := <-failed:
		return err
	case <-time.After(restartGrace):
		return nil
	}
}
