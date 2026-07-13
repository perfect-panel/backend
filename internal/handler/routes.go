package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/svc"
)

func RegisterHandlers(router *server.Hertz, serverCtx *svc.ServiceContext) {
	registerServerPluginRoutes(router, serverCtx)
	registerAdminContentRoutes(router, serverCtx)
	registerAdminOperationsRoutes(router, serverCtx)
	registerAdminManagementRoutes(router, serverCtx)
	registerAuthCommonRoutes(router, serverCtx)
	registerPublicRoutes(router, serverCtx)
}
