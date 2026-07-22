package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminCoupon "github.com/perfect-panel/server/internal/handler/admin/coupon"
	adminOrder "github.com/perfect-panel/server/internal/handler/admin/order"
	adminPayment "github.com/perfect-panel/server/internal/handler/admin/payment"
	adminSubscribe "github.com/perfect-panel/server/internal/handler/admin/subscribe"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminCouponRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/coupon")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminCoupon.CreateCouponHandler(serverCtx))
	group.PUT("/", adminCoupon.UpdateCouponHandler(serverCtx))
	group.DELETE("/", adminCoupon.DeleteCouponHandler(serverCtx))
	group.DELETE("/batch", adminCoupon.BatchDeleteCouponHandler(serverCtx))
	group.GET("/list", adminCoupon.GetCouponListHandler(serverCtx))
}

func registerAdminOrderRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/order")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminOrder.CreateOrderHandler(serverCtx))
	group.GET("/list", adminOrder.GetOrderListHandler(serverCtx))
	group.PUT("/status", adminOrder.UpdateOrderStatusHandler(serverCtx))
}

func registerAdminPaymentRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/payment")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminPayment.CreatePaymentMethodHandler(serverCtx))
	group.PUT("/", adminPayment.UpdatePaymentMethodHandler(serverCtx))
	group.DELETE("/", adminPayment.DeletePaymentMethodHandler(serverCtx))
	group.GET("/list", adminPayment.GetPaymentMethodListHandler(serverCtx))
	group.GET("/platform", adminPayment.GetPaymentPlatformHandler(serverCtx))
}

func registerAdminSubscribeRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/subscribe")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminSubscribe.CreateSubscribeHandler(serverCtx))
	group.PUT("/", adminSubscribe.UpdateSubscribeHandler(serverCtx))
	group.DELETE("/", adminSubscribe.DeleteSubscribeHandler(serverCtx))
	group.DELETE("/batch", adminSubscribe.BatchDeleteSubscribeHandler(serverCtx))
	group.GET("/details", adminSubscribe.GetSubscribeDetailsHandler(serverCtx))
	group.POST("/group", adminSubscribe.CreateSubscribeGroupHandler(serverCtx))
	group.PUT("/group", adminSubscribe.UpdateSubscribeGroupHandler(serverCtx))
	group.DELETE("/group", adminSubscribe.DeleteSubscribeGroupHandler(serverCtx))
	group.DELETE("/group/batch", adminSubscribe.BatchDeleteSubscribeGroupHandler(serverCtx))
	group.GET("/group/list", adminSubscribe.GetSubscribeGroupListHandler(serverCtx))
	group.GET("/list", adminSubscribe.GetSubscribeListHandler(serverCtx))
	group.POST("/reset_all_token", adminSubscribe.ResetAllSubscribeTokenHandler(serverCtx))
	group.POST("/sort", adminSubscribe.SubscribeSortHandler(serverCtx))
}
