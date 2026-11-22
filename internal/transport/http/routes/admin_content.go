package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminApplication "github.com/perfect-panel/backend/internal/module/subscription/transport/http/admin/application"
	adminAds "github.com/perfect-panel/backend/internal/module/support/transport/http/admin/ads"
	adminAnnouncement "github.com/perfect-panel/backend/internal/module/support/transport/http/admin/announcement"
	adminDocument "github.com/perfect-panel/backend/internal/module/support/transport/http/admin/document"
	adminMarketing "github.com/perfect-panel/backend/internal/module/support/transport/http/admin/marketing"
)

func registerAdminAdsRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/ads")
	group.POST("/", adminAds.CreateAdsHandler(deps.Support))
	group.PUT("/", adminAds.UpdateAdsHandler(deps.Support))
	group.DELETE("/", adminAds.DeleteAdsHandler(deps.Support))
	group.GET("/detail", adminAds.GetAdsDetailHandler(deps.Support))
	group.GET("/list", adminAds.GetAdsListHandler(deps.Support))
}

func registerAdminAnnouncementRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/announcement")
	group.POST("/", adminAnnouncement.CreateAnnouncementHandler(deps.Support))
	group.PUT("/", adminAnnouncement.UpdateAnnouncementHandler(deps.Support))
	group.DELETE("/", adminAnnouncement.DeleteAnnouncementHandler(deps.Support))
	group.GET("/detail", adminAnnouncement.GetAnnouncementHandler(deps.Support))
	group.GET("/list", adminAnnouncement.GetAnnouncementListHandler(deps.Support))
}

func registerAdminApplicationRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/application")
	group.POST("/", adminApplication.CreateSubscribeApplicationHandler(deps.Subscription))
	group.GET("/preview", adminApplication.PreviewSubscribeTemplateHandler(deps.Subscription))
	group.PUT("/subscribe_application", adminApplication.UpdateSubscribeApplicationHandler(deps.Subscription))
	group.DELETE("/subscribe_application", adminApplication.DeleteSubscribeApplicationHandler(deps.Subscription))
	group.GET("/subscribe_application_list", adminApplication.GetSubscribeApplicationListHandler(deps.Subscription))
}

func registerAdminDocumentRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/document")
	group.POST("/", adminDocument.CreateDocumentHandler(deps.Support))
	group.PUT("/", adminDocument.UpdateDocumentHandler(deps.Support))
	group.DELETE("/", adminDocument.DeleteDocumentHandler(deps.Support))
	group.DELETE("/batch", adminDocument.BatchDeleteDocumentHandler(deps.Support))
	group.GET("/detail", adminDocument.GetDocumentDetailHandler(deps.Support))
	group.GET("/list", adminDocument.GetDocumentListHandler(deps.Support))
}

func registerAdminMarketingRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/marketing")
	group.GET("/email/batch/list", adminMarketing.GetBatchSendEmailTaskListHandler(deps.Support))
	group.POST("/email/batch/pre-send-count", adminMarketing.GetPreSendEmailCountHandler(deps.Support))
	group.POST("/email/batch/send", adminMarketing.CreateBatchSendEmailTaskHandler(deps.Support))
	group.POST("/email/batch/status", adminMarketing.GetBatchSendEmailTaskStatusHandler(deps.Support))
	group.POST("/email/batch/stop", adminMarketing.StopBatchSendEmailTaskHandler(deps.Support))
	group.POST("/quota/create", adminMarketing.CreateQuotaTaskHandler(deps.Support))
	group.GET("/quota/list", adminMarketing.QueryQuotaTaskListHandler(deps.Support))
	group.POST("/quota/pre-count", adminMarketing.QueryQuotaTaskPreCountHandler(deps.Support))
}
