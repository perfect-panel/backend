package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	serverHandler "github.com/perfect-panel/server/internal/module/network/transport/http/server"
)

func registerServerRoutes(router *server.Hertz, deps Dependencies) {
	serverGroup := router.Group("/v1/server", serverHandler.ServerMiddleware(deps.nodeSecret))
	serverGroup.GET("/config", serverHandler.GetServerConfigHandler(deps.Network))
	serverGroup.POST("/online", serverHandler.PushOnlineUsersHandler(deps.Network))
	serverGroup.POST("/push", serverHandler.ServerPushUserTrafficHandler(deps.Network))
	serverGroup.POST("/status", serverHandler.ServerPushStatusHandler(deps.Network))
	serverGroup.GET("/user", serverHandler.GetServerUserListHandler(deps.Network))
	serverGroup.GET("/alivelist", serverHandler.GetServerAliveListHandler(deps.Network))
	serverGroup.POST("/device", serverHandler.ClaimServerDeviceHandler(deps.Network))

	router.GET("/v2/server/:server_id", serverHandler.QueryServerProtocolConfigHandler(deps.Network, deps.nodeSecret))
}
