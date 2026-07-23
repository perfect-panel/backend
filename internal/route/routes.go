package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/svc"
)

func RegisterHandlers(router *server.Hertz, serverCtx *svc.ServiceContext) {
	registerEdgeRoutes(router, serverCtx)
	registerSubscribeConfigRoutes(router, serverCtx)
	registerServerRoutes(router, serverCtx)

	registerAdminAdsRoutes(router, serverCtx)
	registerAdminAnnouncementRoutes(router, serverCtx)
	registerAdminApplicationRoutes(router, serverCtx)
	registerAdminAuthMethodRoutes(router, serverCtx)
	registerAdminConsoleRoutes(router, serverCtx)
	registerAdminCouponRoutes(router, serverCtx)
	registerAdminDocumentRoutes(router, serverCtx)
	registerAdminLogRoutes(router, serverCtx)
	registerAdminMarketingRoutes(router, serverCtx)
	registerAdminOrderRoutes(router, serverCtx)
	registerAdminPaymentRoutes(router, serverCtx)
	registerAdminServerRoutes(router, serverCtx)
	registerAdminSubscribeRoutes(router, serverCtx)
	registerAdminSystemRoutes(router, serverCtx)
	registerAdminTicketRoutes(router, serverCtx)
	registerAdminToolRoutes(router, serverCtx)
	registerAdminUserRoutes(router, serverCtx)

	registerAuthRoutes(router, serverCtx)
	registerCommonRoutes(router, serverCtx)

	registerPublicAnnouncementRoutes(router, serverCtx)
	registerPublicDocumentRoutes(router, serverCtx)
	registerPublicOrderRoutes(router, serverCtx)
	registerPublicOrderV2Routes(router, serverCtx)
	registerPublicPaymentRoutes(router, serverCtx)
	registerPublicPortalRoutes(router, serverCtx)
	registerPublicSubscribeRoutes(router, serverCtx)
	registerPublicTicketRoutes(router, serverCtx)
	registerPublicUserRoutes(router, serverCtx)
}
