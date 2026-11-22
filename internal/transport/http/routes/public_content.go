package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicPortal "github.com/perfect-panel/backend/internal/module/billing/transport/http/public/portal"
	publicAnnouncement "github.com/perfect-panel/backend/internal/module/support/transport/http/public/announcement"
	publicDocument "github.com/perfect-panel/backend/internal/module/support/transport/http/public/document"
)

func registerPublicAnnouncementRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/announcement")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.GET("/list", publicAnnouncement.QueryAnnouncementHandler(deps.Support))
}

func registerPublicDocumentRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/document")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.GET("/detail", publicDocument.QueryDocumentDetailHandler(deps.Support))
	group.GET("/list", publicDocument.QueryDocumentListHandler(deps.Support))
}

func registerPublicPortalRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/portal")
	group.Use(deps.optionalAuthMiddleware(), deps.deviceMiddleware())
	group.POST("/order/checkout", publicPortal.PurchaseCheckoutHandler(deps.Billing))
	group.GET("/order/status", publicPortal.QueryPurchaseOrderHandler(deps.Billing))
	group.GET("/payment-method", publicPortal.GetAvailablePaymentMethodsHandler(deps.Billing))
	group.POST("/pre", publicPortal.PrePurchaseOrderHandler(deps.Billing))
	group.POST("/purchase", publicPortal.PurchaseHandler(deps.Billing))
	group.GET("/subscribe", publicPortal.GetSubscriptionHandler(deps.Billing))
}
