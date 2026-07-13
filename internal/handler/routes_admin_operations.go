package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminMarketing "github.com/perfect-panel/server/internal/handler/admin/marketing"
	adminOrder "github.com/perfect-panel/server/internal/handler/admin/order"
	adminPayment "github.com/perfect-panel/server/internal/handler/admin/payment"
	adminServer "github.com/perfect-panel/server/internal/handler/admin/server"
	adminSubscribe "github.com/perfect-panel/server/internal/handler/admin/subscribe"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminOperationsRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	adminMarketingGroupRouter := router.Group("/v1/admin/marketing")
	adminMarketingGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Get batch send email task list
		adminMarketingGroupRouter.GET("/email/batch/list", adminMarketing.GetBatchSendEmailTaskListHandler(serverCtx))

		// Get pre-send email count
		adminMarketingGroupRouter.POST("/email/batch/pre-send-count", adminMarketing.GetPreSendEmailCountHandler(serverCtx))

		// Create a batch send email task
		adminMarketingGroupRouter.POST("/email/batch/send", adminMarketing.CreateBatchSendEmailTaskHandler(serverCtx))

		// Get batch send email task status
		adminMarketingGroupRouter.POST("/email/batch/status", adminMarketing.GetBatchSendEmailTaskStatusHandler(serverCtx))

		// Stop a batch send email task
		adminMarketingGroupRouter.POST("/email/batch/stop", adminMarketing.StopBatchSendEmailTaskHandler(serverCtx))

		// Create a quota task
		adminMarketingGroupRouter.POST("/quota/create", adminMarketing.CreateQuotaTaskHandler(serverCtx))

		// Query quota task list
		adminMarketingGroupRouter.GET("/quota/list", adminMarketing.QueryQuotaTaskListHandler(serverCtx))

		// Query quota task pre-count
		adminMarketingGroupRouter.POST("/quota/pre-count", adminMarketing.QueryQuotaTaskPreCountHandler(serverCtx))
	}

	adminOrderGroupRouter := router.Group("/v1/admin/order")
	adminOrderGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create order
		adminOrderGroupRouter.POST("/", adminOrder.CreateOrderHandler(serverCtx))

		// Get order list
		adminOrderGroupRouter.GET("/list", adminOrder.GetOrderListHandler(serverCtx))

		// Update order status
		adminOrderGroupRouter.PUT("/status", adminOrder.UpdateOrderStatusHandler(serverCtx))
	}

	adminPaymentGroupRouter := router.Group("/v1/admin/payment")
	adminPaymentGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create Payment Method
		adminPaymentGroupRouter.POST("/", adminPayment.CreatePaymentMethodHandler(serverCtx))

		// Update Payment Method
		adminPaymentGroupRouter.PUT("/", adminPayment.UpdatePaymentMethodHandler(serverCtx))

		// Delete Payment Method
		adminPaymentGroupRouter.DELETE("/", adminPayment.DeletePaymentMethodHandler(serverCtx))

		// Get Payment Method List
		adminPaymentGroupRouter.GET("/list", adminPayment.GetPaymentMethodListHandler(serverCtx))

		// Get supported payment platform
		adminPaymentGroupRouter.GET("/platform", adminPayment.GetPaymentPlatformHandler(serverCtx))
	}

	adminServerGroupRouter := router.Group("/v1/admin/server")
	adminServerGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create Server
		adminServerGroupRouter.POST("/create", adminServer.CreateServerHandler(serverCtx))

		// Delete Server
		adminServerGroupRouter.POST("/delete", adminServer.DeleteServerHandler(serverCtx))

		// Filter Server List
		adminServerGroupRouter.GET("/list", adminServer.FilterServerListHandler(serverCtx))

		// Create Node
		adminServerGroupRouter.POST("/node/create", adminServer.CreateNodeHandler(serverCtx))

		// Delete Node
		adminServerGroupRouter.POST("/node/delete", adminServer.DeleteNodeHandler(serverCtx))

		// Filter Node List
		adminServerGroupRouter.GET("/node/list", adminServer.FilterNodeListHandler(serverCtx))

		// Reset node sort
		adminServerGroupRouter.POST("/node/sort", adminServer.ResetSortWithNodeHandler(serverCtx))

		// Toggle Node Status
		adminServerGroupRouter.POST("/node/status/toggle", adminServer.ToggleNodeStatusHandler(serverCtx))

		// Query all node tags
		adminServerGroupRouter.GET("/node/tags", adminServer.QueryNodeTagHandler(serverCtx))

		// Get Server Node Config
		adminServerGroupRouter.GET("/node_config", adminServer.GetServerNodeConfigHandler(serverCtx))

		// Update Server Node Config
		adminServerGroupRouter.POST("/node_config/update", adminServer.UpdateServerNodeConfigHandler(serverCtx))

		// Update Node
		adminServerGroupRouter.POST("/node/update", adminServer.UpdateNodeHandler(serverCtx))

		// Get Server Protocols
		adminServerGroupRouter.GET("/protocols", adminServer.GetServerProtocolsHandler(serverCtx))

		// Reset server sort
		adminServerGroupRouter.POST("/server/sort", adminServer.ResetSortWithServerHandler(serverCtx))

		// Update Server
		adminServerGroupRouter.POST("/update", adminServer.UpdateServerHandler(serverCtx))
	}

	adminSubscribeGroupRouter := router.Group("/v1/admin/subscribe")
	adminSubscribeGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create subscribe
		adminSubscribeGroupRouter.POST("/", adminSubscribe.CreateSubscribeHandler(serverCtx))

		// Update subscribe
		adminSubscribeGroupRouter.PUT("/", adminSubscribe.UpdateSubscribeHandler(serverCtx))

		// Delete subscribe
		adminSubscribeGroupRouter.DELETE("/", adminSubscribe.DeleteSubscribeHandler(serverCtx))

		// Batch delete subscribe
		adminSubscribeGroupRouter.DELETE("/batch", adminSubscribe.BatchDeleteSubscribeHandler(serverCtx))

		// Get subscribe details
		adminSubscribeGroupRouter.GET("/details", adminSubscribe.GetSubscribeDetailsHandler(serverCtx))

		// Create subscribe group
		adminSubscribeGroupRouter.POST("/group", adminSubscribe.CreateSubscribeGroupHandler(serverCtx))

		// Update subscribe group
		adminSubscribeGroupRouter.PUT("/group", adminSubscribe.UpdateSubscribeGroupHandler(serverCtx))

		// Delete subscribe group
		adminSubscribeGroupRouter.DELETE("/group", adminSubscribe.DeleteSubscribeGroupHandler(serverCtx))

		// Batch delete subscribe group
		adminSubscribeGroupRouter.DELETE("/group/batch", adminSubscribe.BatchDeleteSubscribeGroupHandler(serverCtx))

		// Get subscribe group list
		adminSubscribeGroupRouter.GET("/group/list", adminSubscribe.GetSubscribeGroupListHandler(serverCtx))

		// Get subscribe list
		adminSubscribeGroupRouter.GET("/list", adminSubscribe.GetSubscribeListHandler(serverCtx))

		// Reset all subscribe tokens
		adminSubscribeGroupRouter.POST("/reset_all_token", adminSubscribe.ResetAllSubscribeTokenHandler(serverCtx))

		// Subscribe sort
		adminSubscribeGroupRouter.POST("/sort", adminSubscribe.SubscribeSortHandler(serverCtx))
	}
}
