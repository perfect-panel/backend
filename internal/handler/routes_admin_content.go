package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminAds "github.com/perfect-panel/server/internal/handler/admin/ads"
	adminAnnouncement "github.com/perfect-panel/server/internal/handler/admin/announcement"
	adminApplication "github.com/perfect-panel/server/internal/handler/admin/application"
	adminAuthMethod "github.com/perfect-panel/server/internal/handler/admin/authMethod"
	adminConsole "github.com/perfect-panel/server/internal/handler/admin/console"
	adminCoupon "github.com/perfect-panel/server/internal/handler/admin/coupon"
	adminDocument "github.com/perfect-panel/server/internal/handler/admin/document"
	adminLog "github.com/perfect-panel/server/internal/handler/admin/log"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminContentRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	adminAdsGroupRouter := router.Group("/v1/admin/ads")
	adminAdsGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create Ads
		adminAdsGroupRouter.POST("/", adminAds.CreateAdsHandler(serverCtx))

		// Update Ads
		adminAdsGroupRouter.PUT("/", adminAds.UpdateAdsHandler(serverCtx))

		// Delete Ads
		adminAdsGroupRouter.DELETE("/", adminAds.DeleteAdsHandler(serverCtx))

		// Get Ads Detail
		adminAdsGroupRouter.GET("/detail", adminAds.GetAdsDetailHandler(serverCtx))

		// Get Ads List
		adminAdsGroupRouter.GET("/list", adminAds.GetAdsListHandler(serverCtx))
	}

	adminAnnouncementGroupRouter := router.Group("/v1/admin/announcement")
	adminAnnouncementGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create announcement
		adminAnnouncementGroupRouter.POST("/", adminAnnouncement.CreateAnnouncementHandler(serverCtx))

		// Update announcement
		adminAnnouncementGroupRouter.PUT("/", adminAnnouncement.UpdateAnnouncementHandler(serverCtx))

		// Delete announcement
		adminAnnouncementGroupRouter.DELETE("/", adminAnnouncement.DeleteAnnouncementHandler(serverCtx))

		// Get announcement
		adminAnnouncementGroupRouter.GET("/detail", adminAnnouncement.GetAnnouncementHandler(serverCtx))

		// Get announcement list
		adminAnnouncementGroupRouter.GET("/list", adminAnnouncement.GetAnnouncementListHandler(serverCtx))
	}

	adminApplicationGroupRouter := router.Group("/v1/admin/application")
	adminApplicationGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create subscribe application
		adminApplicationGroupRouter.POST("/", adminApplication.CreateSubscribeApplicationHandler(serverCtx))

		// Preview Template
		adminApplicationGroupRouter.GET("/preview", adminApplication.PreviewSubscribeTemplateHandler(serverCtx))

		// Update subscribe application
		adminApplicationGroupRouter.PUT("/subscribe_application", adminApplication.UpdateSubscribeApplicationHandler(serverCtx))

		// Delete subscribe application
		adminApplicationGroupRouter.DELETE("/subscribe_application", adminApplication.DeleteSubscribeApplicationHandler(serverCtx))

		// Get subscribe application list
		adminApplicationGroupRouter.GET("/subscribe_application_list", adminApplication.GetSubscribeApplicationListHandler(serverCtx))
	}

	adminAuthMethodGroupRouter := router.Group("/v1/admin/auth-method")
	adminAuthMethodGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Get auth method config
		adminAuthMethodGroupRouter.GET("/config", adminAuthMethod.GetAuthMethodConfigHandler(serverCtx))

		// Update auth method config
		adminAuthMethodGroupRouter.PUT("/config", adminAuthMethod.UpdateAuthMethodConfigHandler(serverCtx))

		// Get email support platform
		adminAuthMethodGroupRouter.GET("/email_platform", adminAuthMethod.GetEmailPlatformHandler(serverCtx))

		// Get auth method list
		adminAuthMethodGroupRouter.GET("/list", adminAuthMethod.GetAuthMethodListHandler(serverCtx))

		// Get sms support platform
		adminAuthMethodGroupRouter.GET("/sms_platform", adminAuthMethod.GetSmsPlatformHandler(serverCtx))

		// Test email send
		adminAuthMethodGroupRouter.POST("/test_email_send", adminAuthMethod.TestEmailSendHandler(serverCtx))

		// Test sms send
		adminAuthMethodGroupRouter.POST("/test_sms_send", adminAuthMethod.TestSmsSendHandler(serverCtx))
	}

	adminConsoleGroupRouter := router.Group("/v1/admin/console")
	adminConsoleGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Query revenue statistics
		adminConsoleGroupRouter.GET("/revenue", adminConsole.QueryRevenueStatisticsHandler(serverCtx))

		// Query server total data
		adminConsoleGroupRouter.GET("/server", adminConsole.QueryServerTotalDataHandler(serverCtx))

		// Query ticket wait reply
		adminConsoleGroupRouter.GET("/ticket", adminConsole.QueryTicketWaitReplyHandler(serverCtx))

		// Query user statistics
		adminConsoleGroupRouter.GET("/user", adminConsole.QueryUserStatisticsHandler(serverCtx))
	}

	adminCouponGroupRouter := router.Group("/v1/admin/coupon")
	adminCouponGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create coupon
		adminCouponGroupRouter.POST("/", adminCoupon.CreateCouponHandler(serverCtx))

		// Update coupon
		adminCouponGroupRouter.PUT("/", adminCoupon.UpdateCouponHandler(serverCtx))

		// Delete coupon
		adminCouponGroupRouter.DELETE("/", adminCoupon.DeleteCouponHandler(serverCtx))

		// Batch delete coupon
		adminCouponGroupRouter.DELETE("/batch", adminCoupon.BatchDeleteCouponHandler(serverCtx))

		// Get coupon list
		adminCouponGroupRouter.GET("/list", adminCoupon.GetCouponListHandler(serverCtx))
	}

	adminDocumentGroupRouter := router.Group("/v1/admin/document")
	adminDocumentGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Create document
		adminDocumentGroupRouter.POST("/", adminDocument.CreateDocumentHandler(serverCtx))

		// Update document
		adminDocumentGroupRouter.PUT("/", adminDocument.UpdateDocumentHandler(serverCtx))

		// Delete document
		adminDocumentGroupRouter.DELETE("/", adminDocument.DeleteDocumentHandler(serverCtx))

		// Batch delete document
		adminDocumentGroupRouter.DELETE("/batch", adminDocument.BatchDeleteDocumentHandler(serverCtx))

		// Get document detail
		adminDocumentGroupRouter.GET("/detail", adminDocument.GetDocumentDetailHandler(serverCtx))

		// Get document list
		adminDocumentGroupRouter.GET("/list", adminDocument.GetDocumentListHandler(serverCtx))
	}

	adminLogGroupRouter := router.Group("/v1/admin/log")
	adminLogGroupRouter.Use(middleware.AuthMiddleware(serverCtx))

	{
		// Filter balance log
		adminLogGroupRouter.GET("/balance/list", adminLog.FilterBalanceLogHandler(serverCtx))

		// Filter commission log
		adminLogGroupRouter.GET("/commission/list", adminLog.FilterCommissionLogHandler(serverCtx))

		// Filter email log
		adminLogGroupRouter.GET("/email/list", adminLog.FilterEmailLogHandler(serverCtx))

		// Filter gift log
		adminLogGroupRouter.GET("/gift/list", adminLog.FilterGiftLogHandler(serverCtx))

		// Filter login log
		adminLogGroupRouter.GET("/login/list", adminLog.FilterLoginLogHandler(serverCtx))

		// Get message log list
		adminLogGroupRouter.GET("/message/list", adminLog.GetMessageLogListHandler(serverCtx))

		// Filter mobile log
		adminLogGroupRouter.GET("/mobile/list", adminLog.FilterMobileLogHandler(serverCtx))

		// Filter register log
		adminLogGroupRouter.GET("/register/list", adminLog.FilterRegisterLogHandler(serverCtx))

		// Filter server traffic log
		adminLogGroupRouter.GET("/server/traffic/list", adminLog.FilterServerTrafficLogHandler(serverCtx))

		// Get log setting
		adminLogGroupRouter.GET("/setting", adminLog.GetLogSettingHandler(serverCtx))

		// Update log setting
		adminLogGroupRouter.POST("/setting", adminLog.UpdateLogSettingHandler(serverCtx))

		// Filter subscribe log
		adminLogGroupRouter.GET("/subscribe/list", adminLog.FilterSubscribeLogHandler(serverCtx))

		// Filter reset subscribe log
		adminLogGroupRouter.GET("/subscribe/reset/list", adminLog.FilterResetSubscribeLogHandler(serverCtx))

		// Filter user subscribe traffic log
		adminLogGroupRouter.GET("/subscribe/traffic/list", adminLog.FilterUserSubscribeTrafficLogHandler(serverCtx))

		// Filter traffic log details
		adminLogGroupRouter.GET("/traffic/details", adminLog.FilterTrafficLogDetailsHandler(serverCtx))
	}
}
