package server

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/network"
)

// handlerFactory compiles only for a factory that takes the service S and
// returns a native Hertz handler.
func handlerFactory[S any](func(S) app.HandlerFunc) {}

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	handlerFactory[network.Service](CreateNodeHandler)
	handlerFactory[network.Service](CreateServerHandler)
	handlerFactory[network.Service](DeleteNodeHandler)
	handlerFactory[network.Service](DeleteServerHandler)
	handlerFactory[network.Service](FilterNodeListHandler)
	handlerFactory[network.Service](FilterServerListHandler)
	handlerFactory[network.Service](GetServerNodeConfigHandler)
	handlerFactory[network.Service](GetServerProtocolsHandler)
	handlerFactory[network.Service](QueryNodeTagHandler)
	handlerFactory[network.Service](ResetSortWithNodeHandler)
	handlerFactory[network.Service](ResetSortWithServerHandler)
	handlerFactory[network.Service](ToggleNodeStatusHandler)
	handlerFactory[network.Service](UpdateNodeHandler)
	handlerFactory[ServerUpdater](UpdateServerHandler)
	handlerFactory[ServerNodeConfigUpdater](UpdateServerNodeConfigHandler)
}
