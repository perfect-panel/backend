package httpserver

import (
	"context"
	"crypto/tls"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/perfect-panel/server/internal/handler"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type Server struct {
	h *server.Hertz
}

func New(svc *svc.ServiceContext, addr string, tlsConfig *tls.Config) *Server {
	opts := []config.Option{
		server.WithHostPorts(addr),
		server.WithDisablePrintRoute(true),
	}
	if tlsConfig != nil {
		opts = append(opts, server.WithTLS(tlsConfig))
	}

	return newServer(svc, opts)
}

func newServer(svc *svc.ServiceContext, opts []config.Option) *Server {
	engine := server.Default(opts...)
	engine.Use(middleware.TraceMiddleware(svc), middleware.LoggerMiddleware(svc), middleware.CorsMiddleware)

	handler.RegisterHandlers(engine, svc)
	handler.RegisterTelegramHandlers(engine, svc)
	handler.RegisterNotifyHandlers(engine, svc)

	// 注册插件路由
	if mgr, ok := svc.PluginMgr.(*plugin.Manager); ok {
		handler.RegisterPluginHandlers(engine, svc, mgr)
	}

	return &Server{h: engine}
}

func (s *Server) Start() {
	if err := s.h.Run(); err != nil {
		logger.Errorf("server start error: %s", err.Error())
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.h.Shutdown(ctx)
}

func (s *Server) Engine() *server.Hertz {
	return s.h
}
