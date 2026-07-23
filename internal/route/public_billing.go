package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicOrder "github.com/perfect-panel/server/internal/handler/public/order"
	publicPayment "github.com/perfect-panel/server/internal/handler/public/payment"
	publicSubscribe "github.com/perfect-panel/server/internal/handler/public/subscribe"
	publicTicket "github.com/perfect-panel/server/internal/handler/public/ticket"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerPublicOrderRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/order")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.POST("/close", publicOrder.CloseOrderHandler(serverCtx))
	group.GET("/detail", publicOrder.QueryOrderDetailHandler(serverCtx))
	group.GET("/list", publicOrder.QueryOrderListHandler(serverCtx))
	group.POST("/pre", publicOrder.PreCreateOrderHandler(serverCtx))
	group.POST("/purchase", publicOrder.PurchaseHandler(serverCtx))
	group.POST("/recharge", publicOrder.RechargeHandler(serverCtx))
	group.POST("/renewal", publicOrder.RenewalHandler(serverCtx))
	group.POST("/reset", publicOrder.ResetTrafficHandler(serverCtx))
}

func registerPublicOrderV2Routes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v2/public/orders")
	group.Use(middleware.OptionalAuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.POST("", publicOrder.V2CreateAndCheckoutHandler(serverCtx))
	group.POST("/:orderNo/checkout", publicOrder.V2CheckoutHandler(serverCtx))
	group.GET("/:orderNo", publicOrder.V2GetOrderHandler(serverCtx))
	group.POST("/:orderNo/event-ticket", publicOrder.V2EventTicketHandler(serverCtx))
	group.POST("/:orderNo/session", publicOrder.V2OrderSessionHandler(serverCtx))

	// EventSource cannot participate in the native-device response encryption
	// protocol. The short-lived stream ticket is the authorization mechanism
	// for this browser-facing route, so do not apply DeviceMiddleware here.
	streamGroup := router.Group("/v2/public/orders")
	streamGroup.Use(middleware.OptionalAuthMiddleware(serverCtx))
	streamGroup.GET("/:orderNo/events", publicOrder.V2OrderEventsHandler(serverCtx))
}

func registerPublicPaymentRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/payment")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.GET("/methods", publicPayment.GetAvailablePaymentMethodsHandler(serverCtx))
}

func registerPublicSubscribeRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/subscribe")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.GET("/list", publicSubscribe.QuerySubscribeListHandler(serverCtx))
	group.GET("/node/list", publicSubscribe.QueryUserSubscribeNodeListHandler(serverCtx))
}

func registerPublicTicketRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/public/ticket")
	group.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	group.PUT("/", publicTicket.UpdateUserTicketStatusHandler(serverCtx))
	group.POST("/", publicTicket.CreateUserTicketHandler(serverCtx))
	group.GET("/detail", publicTicket.GetUserTicketDetailsHandler(serverCtx))
	group.POST("/follow", publicTicket.CreateUserTicketFollowHandler(serverCtx))
	group.GET("/list", publicTicket.GetUserTicketListHandler(serverCtx))
}
