package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicAnnouncement "github.com/perfect-panel/server/internal/handler/public/announcement"
	publicDocument "github.com/perfect-panel/server/internal/handler/public/document"
	publicOrder "github.com/perfect-panel/server/internal/handler/public/order"
	publicPayment "github.com/perfect-panel/server/internal/handler/public/payment"
	publicPortal "github.com/perfect-panel/server/internal/handler/public/portal"
	publicSubscribe "github.com/perfect-panel/server/internal/handler/public/subscribe"
	publicTicket "github.com/perfect-panel/server/internal/handler/public/ticket"
	publicUser "github.com/perfect-panel/server/internal/handler/public/user"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerPublicRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	publicAnnouncementGroupRouter := router.Group("/v1/public/announcement")
	publicAnnouncementGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicAnnouncementGroupRouter.GET("/list", publicAnnouncement.QueryAnnouncementHandler(serverCtx))

	publicDocumentGroupRouter := router.Group("/v1/public/document")
	publicDocumentGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicDocumentGroupRouter.GET("/detail", publicDocument.QueryDocumentDetailHandler(serverCtx))
	publicDocumentGroupRouter.GET("/list", publicDocument.QueryDocumentListHandler(serverCtx))

	publicOrderGroupRouter := router.Group("/v1/public/order")
	publicOrderGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicOrderGroupRouter.POST("/close", publicOrder.CloseOrderHandler(serverCtx))
	publicOrderGroupRouter.GET("/detail", publicOrder.QueryOrderDetailHandler(serverCtx))
	publicOrderGroupRouter.GET("/list", publicOrder.QueryOrderListHandler(serverCtx))
	publicOrderGroupRouter.POST("/pre", publicOrder.PreCreateOrderHandler(serverCtx))
	publicOrderGroupRouter.POST("/purchase", publicOrder.PurchaseHandler(serverCtx))
	publicOrderGroupRouter.POST("/recharge", publicOrder.RechargeHandler(serverCtx))
	publicOrderGroupRouter.POST("/renewal", publicOrder.RenewalHandler(serverCtx))
	publicOrderGroupRouter.POST("/reset", publicOrder.ResetTrafficHandler(serverCtx))

	publicPaymentGroupRouter := router.Group("/v1/public/payment")
	publicPaymentGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicPaymentGroupRouter.GET("/methods", publicPayment.GetAvailablePaymentMethodsHandler(serverCtx))

	publicPortalGroupRouter := router.Group("/v1/public/portal")
	publicPortalGroupRouter.Use(middleware.DeviceMiddleware(serverCtx))
	publicPortalGroupRouter.POST("/order/checkout", publicPortal.PurchaseCheckoutHandler(serverCtx))
	publicPortalGroupRouter.GET("/order/status", publicPortal.QueryPurchaseOrderHandler(serverCtx))
	publicPortalGroupRouter.GET("/payment-method", publicPortal.GetAvailablePaymentMethodsHandler(serverCtx))
	publicPortalGroupRouter.POST("/pre", publicPortal.PrePurchaseOrderHandler(serverCtx))
	publicPortalGroupRouter.POST("/purchase", publicPortal.PurchaseHandler(serverCtx))
	publicPortalGroupRouter.GET("/subscribe", publicPortal.GetSubscriptionHandler(serverCtx))

	publicSubscribeGroupRouter := router.Group("/v1/public/subscribe")
	publicSubscribeGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicSubscribeGroupRouter.GET("/list", publicSubscribe.QuerySubscribeListHandler(serverCtx))
	publicSubscribeGroupRouter.GET("/node/list", publicSubscribe.QueryUserSubscribeNodeListHandler(serverCtx))

	publicTicketGroupRouter := router.Group("/v1/public/ticket")
	publicTicketGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicTicketGroupRouter.PUT("/", publicTicket.UpdateUserTicketStatusHandler(serverCtx))
	publicTicketGroupRouter.POST("/", publicTicket.CreateUserTicketHandler(serverCtx))
	publicTicketGroupRouter.GET("/detail", publicTicket.GetUserTicketDetailsHandler(serverCtx))
	publicTicketGroupRouter.POST("/follow", publicTicket.CreateUserTicketFollowHandler(serverCtx))
	publicTicketGroupRouter.GET("/list", publicTicket.GetUserTicketListHandler(serverCtx))

	publicUserGroupRouter := router.Group("/v1/public/user")
	publicUserGroupRouter.Use(middleware.AuthMiddleware(serverCtx), middleware.DeviceMiddleware(serverCtx))
	publicUserGroupRouter.GET("/affiliate/count", publicUser.QueryUserAffiliateHandler(serverCtx))
	publicUserGroupRouter.GET("/affiliate/list", publicUser.QueryUserAffiliateListHandler(serverCtx))
	publicUserGroupRouter.GET("/balance_log", publicUser.QueryUserBalanceLogHandler(serverCtx))
	publicUserGroupRouter.PUT("/bind_email", publicUser.UpdateBindEmailHandler(serverCtx))
	publicUserGroupRouter.PUT("/bind_mobile", publicUser.UpdateBindMobileHandler(serverCtx))
	publicUserGroupRouter.POST("/bind_oauth", publicUser.BindOAuthHandler(serverCtx))
	publicUserGroupRouter.POST("/bind_oauth/callback", publicUser.BindOAuthCallbackHandler(serverCtx))
	publicUserGroupRouter.GET("/bind_telegram", publicUser.BindTelegramHandler(serverCtx))
	publicUserGroupRouter.GET("/commission_log", publicUser.QueryUserCommissionLogHandler(serverCtx))
	publicUserGroupRouter.POST("/commission_withdraw", publicUser.CommissionWithdrawHandler(serverCtx))
	publicUserGroupRouter.GET("/devices", publicUser.GetDeviceListHandler(serverCtx))
	publicUserGroupRouter.GET("/info", publicUser.QueryUserInfoHandler(serverCtx))
	publicUserGroupRouter.GET("/login_log", publicUser.GetLoginLogHandler(serverCtx))
	publicUserGroupRouter.PUT("/notify", publicUser.UpdateUserNotifyHandler(serverCtx))
	publicUserGroupRouter.GET("/oauth_methods", publicUser.GetOAuthMethodsHandler(serverCtx))
	publicUserGroupRouter.PUT("/password", publicUser.UpdateUserPasswordHandler(serverCtx))
	publicUserGroupRouter.PUT("/rules", publicUser.UpdateUserRulesHandler(serverCtx))
	publicUserGroupRouter.GET("/subscribe", publicUser.QueryUserSubscribeHandler(serverCtx))
	publicUserGroupRouter.GET("/subscribe_log", publicUser.GetSubscribeLogHandler(serverCtx))
	publicUserGroupRouter.PUT("/subscribe_note", publicUser.UpdateUserSubscribeNoteHandler(serverCtx))
	publicUserGroupRouter.PUT("/subscribe_token", publicUser.ResetUserSubscribeTokenHandler(serverCtx))
	publicUserGroupRouter.PUT("/unbind_device", publicUser.UnbindDeviceHandler(serverCtx))
	publicUserGroupRouter.POST("/unbind_oauth", publicUser.UnbindOAuthHandler(serverCtx))
	publicUserGroupRouter.POST("/unbind_telegram", publicUser.UnbindTelegramHandler(serverCtx))
	publicUserGroupRouter.POST("/unsubscribe", publicUser.UnsubscribeHandler(serverCtx))
	publicUserGroupRouter.POST("/unsubscribe/pre", publicUser.PreUnsubscribeHandler(serverCtx))
	publicUserGroupRouter.POST("/verify_email", publicUser.VerifyEmailHandler(serverCtx))
	publicUserGroupRouter.GET("/withdrawal_log", publicUser.QueryWithdrawalLogHandler(serverCtx))
}
