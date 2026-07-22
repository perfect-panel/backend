package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminAds "github.com/perfect-panel/server/internal/handler/admin/ads"
	adminAnnouncement "github.com/perfect-panel/server/internal/handler/admin/announcement"
	adminApplication "github.com/perfect-panel/server/internal/handler/admin/application"
	adminDocument "github.com/perfect-panel/server/internal/handler/admin/document"
	adminMarketing "github.com/perfect-panel/server/internal/handler/admin/marketing"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminAdsRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/ads")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminAds.CreateAdsHandler(serverCtx))
	group.PUT("/", adminAds.UpdateAdsHandler(serverCtx))
	group.DELETE("/", adminAds.DeleteAdsHandler(serverCtx))
	group.GET("/detail", adminAds.GetAdsDetailHandler(serverCtx))
	group.GET("/list", adminAds.GetAdsListHandler(serverCtx))
}

func registerAdminAnnouncementRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/announcement")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminAnnouncement.CreateAnnouncementHandler(serverCtx))
	group.PUT("/", adminAnnouncement.UpdateAnnouncementHandler(serverCtx))
	group.DELETE("/", adminAnnouncement.DeleteAnnouncementHandler(serverCtx))
	group.GET("/detail", adminAnnouncement.GetAnnouncementHandler(serverCtx))
	group.GET("/list", adminAnnouncement.GetAnnouncementListHandler(serverCtx))
}

func registerAdminApplicationRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/application")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminApplication.CreateSubscribeApplicationHandler(serverCtx))
	group.GET("/preview", adminApplication.PreviewSubscribeTemplateHandler(serverCtx))
	group.PUT("/subscribe_application", adminApplication.UpdateSubscribeApplicationHandler(serverCtx))
	group.DELETE("/subscribe_application", adminApplication.DeleteSubscribeApplicationHandler(serverCtx))
	group.GET("/subscribe_application_list", adminApplication.GetSubscribeApplicationListHandler(serverCtx))
}

func registerAdminDocumentRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/document")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/", adminDocument.CreateDocumentHandler(serverCtx))
	group.PUT("/", adminDocument.UpdateDocumentHandler(serverCtx))
	group.DELETE("/", adminDocument.DeleteDocumentHandler(serverCtx))
	group.DELETE("/batch", adminDocument.BatchDeleteDocumentHandler(serverCtx))
	group.GET("/detail", adminDocument.GetDocumentDetailHandler(serverCtx))
	group.GET("/list", adminDocument.GetDocumentListHandler(serverCtx))
}

func registerAdminMarketingRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/marketing")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/email/batch/list", adminMarketing.GetBatchSendEmailTaskListHandler(serverCtx))
	group.POST("/email/batch/pre-send-count", adminMarketing.GetPreSendEmailCountHandler(serverCtx))
	group.POST("/email/batch/send", adminMarketing.CreateBatchSendEmailTaskHandler(serverCtx))
	group.POST("/email/batch/status", adminMarketing.GetBatchSendEmailTaskStatusHandler(serverCtx))
	group.POST("/email/batch/stop", adminMarketing.StopBatchSendEmailTaskHandler(serverCtx))
	group.POST("/quota/create", adminMarketing.CreateQuotaTaskHandler(serverCtx))
	group.GET("/quota/list", adminMarketing.QueryQuotaTaskListHandler(serverCtx))
	group.POST("/quota/pre-count", adminMarketing.QueryQuotaTaskPreCountHandler(serverCtx))
}
