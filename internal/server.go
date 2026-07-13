package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/transport/httpserver"
	"github.com/perfect-panel/server/pkg/logger"

	"github.com/perfect-panel/server/pkg/proc"
	"github.com/perfect-panel/server/pkg/trace"

	"github.com/perfect-panel/server/internal/svc"
)

type Service struct {
	server transportServer
	svc    *svc.ServiceContext
}

func NewService(svc *svc.ServiceContext) *Service {
	return &Service{
		svc: svc,
	}
}

type transportServer interface {
	Start()
	Shutdown(ctx context.Context) error
}

func newTransportServer(svc *svc.ServiceContext, addr string) transportServer {
	var tlsConfig *tls.Config
	if svc.Config.TLS.Enable {
		cert, err := tls.LoadX509KeyPair(svc.Config.TLS.CertFile, svc.Config.TLS.KeyFile)
		if err != nil {
			logger.Errorf("load tls certificate error: %s", err.Error())
			return nil
		}
		tlsConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
	}
	return httpserver.New(svc, addr, tlsConfig)
}

func (m *Service) Start() {
	if m.svc == nil {
		panic("config file path is nil")
	}

	// 等待插件管理器加载完成
	if m.svc.PluginReady != nil {
		if err := m.svc.PluginReady.WaitReady(context.Background()); err != nil {
			logger.Errorf("plugin manager not ready: %s", err.Error())
		}
	}

	// get server port
	port := m.svc.Config.Port
	host := m.svc.Config.Host
	// check gateway mode
	if report.IsGatewayMode() {
		// get free port
		freePort, err := report.ModulePort()
		if err != nil {
			logger.Errorf("get module port error: %s", err.Error())
			panic(err)
		}
		port = freePort
		host = "127.0.0.1"
		// register module
		err = report.RegisterModule(port)
		if err != nil {
			logger.Errorf("register module error: %s", err.Error())
			os.Exit(1)
		}
		logger.Infof("module registered on port %d", port)
	}

	serverAddr := fmt.Sprintf("%v:%d", host, port)
	initialize.StartInitSystemConfig(m.svc)
	if err := m.svc.Store.User().ValidateEmailIdentityUniqueness(context.Background()); err != nil {
		panic(err.Error())
	}
	m.server = newTransportServer(m.svc, serverAddr)
	if m.server == nil {
		return
	}
	trace.StartAgent(trace.Config{
		Name:    "ppanel",
		Sampler: 1.0,
		Batcher: "",
	})
	proc.AddShutdownListener(func() {
		trace.StopAgent()
	})
	m.svc.Restart = m.Restart
	logger.Infof("server start at %v", serverAddr)
	m.server.Start()
}

func (m *Service) Stop() {
	if m.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.server.Shutdown(ctx); err != nil {
		logger.Errorf("server shutdown error: %s", err.Error())
	}
	logger.Info("server shutdown")
}

func (m *Service) Restart() error {
	if m.server == nil {
		return errors.New("server is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.server.Shutdown(ctx); err != nil {
		logger.Errorf("server shutdown error: %v", err.Error())
		return err
	}
	logger.Info("server shutdown")
	go m.Start()
	return nil
}
