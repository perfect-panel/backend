package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicOrder "github.com/perfect-panel/backend/internal/module/billing/transport/http/public/order"
	publicPayment "github.com/perfect-panel/backend/internal/module/billing/transport/http/public/payment"
	publicSubscribe "github.com/perfect-panel/backend/internal/module/subscription/transport/http/public/subscribe"
	publicTicket "github.com/perfect-panel/backend/internal/module/support/transport/http/public/ticket"
)

func registerPublicOrderRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/order")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.POST("/close", publicOrder.CloseOrderHandler(deps.Billing))
	group.GET("/detail", publicOrder.QueryOrderDetailHandler(deps.Billing))
	group.GET("/list", publicOrder.QueryOrderListHandler(deps.Billing))
	group.POST("/pre", publicOrder.PreCreateOrderHandler(deps.Billing))
	group.POST("/purchase", publicOrder.PurchaseHandler(deps.Billing))
	group.POST("/recharge", publicOrder.RechargeHandler(deps.Billing))
	group.POST("/renewal", publicOrder.RenewalHandler(deps.Billing))
	group.POST("/reset", publicOrder.ResetTrafficHandler(deps.Billing))
}

func registerPublicOrderV2Routes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v2/public/orders")
	group.Use(deps.optionalAuthMiddleware(), deps.deviceMiddleware())
	group.POST("", publicOrder.V2CreateAndCheckoutHandler(deps.Billing))
	group.POST("/:orderNo/checkout", publicOrder.V2CheckoutHandler(deps.Billing))
	group.GET("/:orderNo", publicOrder.V2GetOrderHandler(deps.Billing))
	group.POST("/:orderNo/event-ticket", publicOrder.V2EventTicketHandler(deps.Billing))
	group.POST("/:orderNo/session", publicOrder.V2OrderSessionHandler(deps.Billing))

	// EventSource cannot participate in the native-device response encryption
	// protocol. The short-lived stream ticket is the authorization mechanism
	// for this browser-facing route, so do not apply DeviceMiddleware here.
	streamGroup := router.Group("/v2/public/orders")
	streamGroup.Use(deps.optionalAuthMiddleware())
	streamGroup.GET("/:orderNo/events", publicOrder.V2OrderEventsHandler(publicOrder.EventStreamDeps{
		Billing: deps.Billing,
	}))
}

func registerPublicPaymentRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/payment")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.GET("/methods", publicPayment.GetAvailablePaymentMethodsHandler(deps.Billing))
}

func registerPublicSubscribeRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/subscribe")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.GET("/list", publicSubscribe.QuerySubscribeListHandler(deps.Subscription))
	group.GET("/node/list", publicSubscribe.QueryUserSubscribeNodeListHandler(deps.Subscription))
}

func registerPublicTicketRoutes(router *server.Hertz, deps Dependencies) {
	group := router.Group("/v1/public/ticket")
	group.Use(deps.authMiddleware(), deps.deviceMiddleware())
	group.PUT("/", publicTicket.UpdateUserTicketStatusHandler(deps.Support))
	group.POST("/", publicTicket.CreateUserTicketHandler(deps.Support))
	group.GET("/detail", publicTicket.GetUserTicketDetailsHandler(deps.Support))
	group.POST("/follow", publicTicket.CreateUserTicketFollowHandler(deps.Support))
	group.GET("/list", publicTicket.GetUserTicketListHandler(deps.Support))
}
