package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicAnnouncement "github.com/perfect-panel/server/internal/handler/public/announcement"
	publicDocument "github.com/perfect-panel/server/internal/handler/public/document"
	publicPortal "github.com/perfect-panel/server/internal/handler/public/portal"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerPublicAnnouncementRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/announcement")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.GET("/list", publicAnnouncement.QueryAnnouncementHandler(serverCtx))
}

func registerPublicDocumentRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/document")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.GET("/detail", publicDocument.QueryDocumentDetailHandler(serverCtx))
	group.GET("/list", publicDocument.QueryDocumentListHandler(serverCtx))
}

func registerPublicPortalRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/portal")
	group.Use(middleware.OptionalAuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.POST("/order/checkout", publicPortal.PurchaseCheckoutHandler(serverCtx))
	group.GET("/order/status", publicPortal.QueryPurchaseOrderHandler(serverCtx))
	group.GET("/payment-method", publicPortal.GetAvailablePaymentMethodsHandler(serverCtx))
	group.POST("/pre", publicPortal.PrePurchaseOrderHandler(serverCtx))
	group.POST("/purchase", publicPortal.PurchaseHandler(serverCtx))
	group.GET("/subscribe", publicPortal.GetSubscriptionHandler(serverCtx))
}
