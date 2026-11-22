package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	edge "github.com/perfect-panel/backend/internal/module/network/transport/http/edge"
)

func registerEdgeRoutes(router *server.Hertz, deps Dependencies) {
	if !deps.runtimeConfig().EdgeSubscribe.Enabled {
		return
	}
	router.GET("/api/edge/v1/manifest", edge.ManifestHandler(edge.ManifestDeps{
		Network: deps.Network,
		Redis:   deps.Redis,
		Config:  deps.edgeSubscribeConfig,
	}))
}
