// Package httpserver builds the Hertz server of the HTTP API: the middleware
// every request passes (tracing, request logging, CORS), the routes of
// package routes, and the Telegram webhook and payment notify handlers that
// their modules register.
package httpserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/perfect-panel/backend/internal/config"
	billingHTTP "github.com/perfect-panel/backend/internal/module/billing/transport/http"
	"github.com/perfect-panel/backend/internal/module/notification"
	notificationHTTP "github.com/perfect-panel/backend/internal/module/notification/transport/http"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
	"github.com/perfect-panel/backend/internal/transport/http/routes"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

type Server struct {
	h *server.Hertz
	// shuttingDown records that Shutdown was called, so Start tells the
	// listener closing on purpose from one that failed.
	shuttingDown atomic.Bool
}

type Dependencies struct {
	Routes           routes.Dependencies
	Notification     notification.Service
	TelegramBotToken func() string
	RequestMetadata  requestmeta.Enricher
	// TrustedProxies lists the reverse proxies (IP addresses or CIDRs) whose
	// X-Forwarded-For and X-Real-IP headers name the client. Empty believes
	// no header: the client address is the connection's remote address.
	TrustedProxies []string
	// AllowedOrigins lists the browser origins CORS admits, as
	// scheme://host[:port]. Empty reflects the request's Origin.
	AllowedOrigins []string
	// HTTP bounds the listener; a zero field leaves Hertz's default.
	HTTP config.HTTPConfig
}

func New(deps Dependencies, addr string, tlsConfig *tls.Config) *Server {
	opts := []hertzconfig.Option{
		server.WithHostPorts(addr),
		server.WithDisablePrintRoute(true),
	}
	opts = append(opts, listenerOptions(deps.HTTP)...)
	if tlsConfig != nil {
		opts = append(opts, server.WithTLS(tlsConfig))
	}

	return newServer(deps, opts)
}

// listenerOptions turns the configured listener bounds into Hertz options.
// Only the bounds set are passed on, so an unconfigured deployment runs with
// the defaults it always had; the write timeout in particular stays
// unbounded unless configured, which the streaming endpoints need.
func listenerOptions(cfg config.HTTPConfig) []hertzconfig.Option {
	var opts []hertzconfig.Option
	if cfg.ReadTimeoutSeconds > 0 {
		opts = append(opts, server.WithReadTimeout(time.Duration(cfg.ReadTimeoutSeconds)*time.Second))
	}
	if cfg.WriteTimeoutSeconds > 0 {
		opts = append(opts, server.WithWriteTimeout(time.Duration(cfg.WriteTimeoutSeconds)*time.Second))
	}
	if cfg.IdleTimeoutSeconds > 0 {
		opts = append(opts, server.WithIdleTimeout(time.Duration(cfg.IdleTimeoutSeconds)*time.Second))
	}
	if cfg.MaxRequestBodyMB > 0 {
		opts = append(opts, server.WithMaxRequestBodySize(cfg.MaxRequestBodyMB<<20))
	}
	return opts
}

func newServer(deps Dependencies, opts []hertzconfig.Option) *Server {
	engine := server.Default(opts...)
	// The client address every middleware and handler reads (rate limits,
	// audit logs, device records) believes a forwarding header only from a
	// configured proxy; a misspelled entry is refused, never widened.
	trusted, err := ParseTrustedProxies(deps.TrustedProxies)
	if err != nil {
		logger.Errorf("trusted proxies: %v; the invalid entries are ignored", err)
	}
	resolveClientIP := clientIPFunc(trusted)
	// The engine hands the resolver to the contexts it pools for the
	// listener; the middleware installs it on every context served, so a
	// context built another way (tests, embedded serving) resolves the same.
	engine.SetClientIPFunc(resolveClientIP)
	engine.Use(func(c context.Context, ctx *app.RequestContext) {
		ctx.SetClientIPFunc(resolveClientIP)
		ctx.Next(c)
	}, middleware.TraceMiddleware(), middleware.LoggerMiddleware(deps.RequestMetadata), middleware.NewCorsMiddleware(deps.AllowedOrigins))

	routes.RegisterHandlers(engine, deps.Routes)
	notificationHTTP.RegisterTelegramHandlers(engine, deps.Notification, deps.TelegramBotToken)
	billingHTTP.RegisterNotifyHandlers(engine, deps.Routes.Billing)

	return &Server{h: engine}
}

// Start serves until Shutdown and returns the error that stopped the server
// before then, a listener that could not bind above all. It used to log that
// error only, leaving a process that consumed tasks with no API to serve
// them; the caller decides now, and fails fast.
func (s *Server) Start() (err error) {
	// Hertz's netpoll transport panics instead of returning the error when
	// the listener cannot bind; the standard transport returns it. Both
	// reach the caller as the error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("http server: %v", r)
			logger.Errorf("server start error: %s", err.Error())
		}
	}()
	err = s.h.Run()
	if err == nil || s.shuttingDown.Load() {
		// The listener closed because Shutdown asked it to.
		return nil
	}
	logger.Errorf("server start error: %s", err.Error())
	return err
}

// Shutdown stops accepting connections and waits for the open requests
// until ctx ends; Start returns nil afterwards.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shuttingDown.Store(true)
	return s.h.Shutdown(ctx)
}

func (s *Server) Engine() *server.Hertz {
	return s.h
}
