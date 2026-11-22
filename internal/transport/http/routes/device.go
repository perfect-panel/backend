package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/transport/devicesocket"
)

// registerDeviceRoutes serves the device WebSocket behind the session
// middleware only: the device transport's encrypted envelope would break the
// WebSocket handshake, so deviceMiddleware is deliberately absent. The
// handler refuses a foreign user id itself.
func registerDeviceRoutes(router *server.Hertz, deps Dependencies) {
	router.GET("/v1/app/ws/:userid/:identifier", deps.authMiddleware(),
		devicesocket.Handler(deps.Devices, devicesocket.HandlerDeps{MaxDevices: deps.DeviceLimit}))
}
