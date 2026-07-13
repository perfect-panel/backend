package server

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateNodeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = CreateServerHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = DeleteNodeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = DeleteServerHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = FilterNodeListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = FilterServerListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetServerNodeConfigHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = GetServerProtocolsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = QueryNodeTagHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ResetSortWithNodeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ResetSortWithServerHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ToggleNodeStatusHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateNodeHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateServerHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = UpdateServerNodeConfigHandler
}
