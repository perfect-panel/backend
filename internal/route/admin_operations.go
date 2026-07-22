package route

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminAuthMethod "github.com/perfect-panel/server/internal/handler/admin/authMethod"
	adminConsole "github.com/perfect-panel/server/internal/handler/admin/console"
	adminLog "github.com/perfect-panel/server/internal/handler/admin/log"
	adminServer "github.com/perfect-panel/server/internal/handler/admin/server"
	adminSystem "github.com/perfect-panel/server/internal/handler/admin/system"
	adminTicket "github.com/perfect-panel/server/internal/handler/admin/ticket"
	adminTool "github.com/perfect-panel/server/internal/handler/admin/tool"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminAuthMethodRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/auth-method")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/config", adminAuthMethod.GetAuthMethodConfigHandler(serverCtx))
	group.PUT("/config", adminAuthMethod.UpdateAuthMethodConfigHandler(serverCtx))
	group.GET("/email_platform", adminAuthMethod.GetEmailPlatformHandler(serverCtx))
	group.GET("/list", adminAuthMethod.GetAuthMethodListHandler(serverCtx))
	group.GET("/sms_platform", adminAuthMethod.GetSmsPlatformHandler(serverCtx))
	group.POST("/test_email_send", adminAuthMethod.TestEmailSendHandler(serverCtx))
	group.POST("/test_sms_send", adminAuthMethod.TestSmsSendHandler(serverCtx))
}

func registerAdminConsoleRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/console")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/revenue", adminConsole.QueryRevenueStatisticsHandler(serverCtx))
	group.GET("/server", adminConsole.QueryServerTotalDataHandler(serverCtx))
	group.GET("/ticket", adminConsole.QueryTicketWaitReplyHandler(serverCtx))
	group.GET("/user", adminConsole.QueryUserStatisticsHandler(serverCtx))
}

func registerAdminLogRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/log")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/balance/list", adminLog.FilterBalanceLogHandler(serverCtx))
	group.GET("/commission/list", adminLog.FilterCommissionLogHandler(serverCtx))
	group.GET("/email/list", adminLog.FilterEmailLogHandler(serverCtx))
	group.GET("/gift/list", adminLog.FilterGiftLogHandler(serverCtx))
	group.GET("/login/list", adminLog.FilterLoginLogHandler(serverCtx))
	group.GET("/message/list", adminLog.GetMessageLogListHandler(serverCtx))
	group.GET("/mobile/list", adminLog.FilterMobileLogHandler(serverCtx))
	group.GET("/register/list", adminLog.FilterRegisterLogHandler(serverCtx))
	group.GET("/server/traffic/list", adminLog.FilterServerTrafficLogHandler(serverCtx))
	group.GET("/setting", adminLog.GetLogSettingHandler(serverCtx))
	group.POST("/setting", adminLog.UpdateLogSettingHandler(serverCtx))
	group.GET("/subscribe/list", adminLog.FilterSubscribeLogHandler(serverCtx))
	group.GET("/subscribe/reset/list", adminLog.FilterResetSubscribeLogHandler(serverCtx))
	group.GET("/subscribe/traffic/list", adminLog.FilterUserSubscribeTrafficLogHandler(serverCtx))
	group.GET("/traffic/details", adminLog.FilterTrafficLogDetailsHandler(serverCtx))
}

func registerAdminServerRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/server")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.POST("/create", adminServer.CreateServerHandler(serverCtx))
	group.POST("/delete", adminServer.DeleteServerHandler(serverCtx))
	group.GET("/list", adminServer.FilterServerListHandler(serverCtx))
	group.POST("/node/create", adminServer.CreateNodeHandler(serverCtx))
	group.POST("/node/delete", adminServer.DeleteNodeHandler(serverCtx))
	group.GET("/node/list", adminServer.FilterNodeListHandler(serverCtx))
	group.POST("/node/sort", adminServer.ResetSortWithNodeHandler(serverCtx))
	group.POST("/node/status/toggle", adminServer.ToggleNodeStatusHandler(serverCtx))
	group.GET("/node/tags", adminServer.QueryNodeTagHandler(serverCtx))
	group.GET("/node_config", adminServer.GetServerNodeConfigHandler(serverCtx))
	group.POST("/node_config/update", adminServer.UpdateServerNodeConfigHandler(serverCtx))
	group.POST("/node/update", adminServer.UpdateNodeHandler(serverCtx))
	group.GET("/protocols", adminServer.GetServerProtocolsHandler(serverCtx))
	group.POST("/server/sort", adminServer.ResetSortWithServerHandler(serverCtx))
	group.POST("/update", adminServer.UpdateServerHandler(serverCtx))
}

func registerAdminSystemRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/system")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/currency_config", adminSystem.GetCurrencyConfigHandler(serverCtx))
	group.PUT("/currency_config", adminSystem.UpdateCurrencyConfigHandler(serverCtx))
	group.GET("/get_node_multiplier", adminSystem.GetNodeMultiplierHandler(serverCtx))
	group.GET("/invite_config", adminSystem.GetInviteConfigHandler(serverCtx))
	group.PUT("/invite_config", adminSystem.UpdateInviteConfigHandler(serverCtx))
	group.GET("/module", adminSystem.GetModuleConfigHandler(serverCtx))
	group.GET("/node_config", adminSystem.GetNodeConfigHandler(serverCtx))
	group.PUT("/node_config", adminSystem.UpdateNodeConfigHandler(serverCtx))
	group.GET("/node_multiplier/preview", adminSystem.PreViewNodeMultiplierHandler(serverCtx))
	group.GET("/privacy", adminSystem.GetPrivacyPolicyConfigHandler(serverCtx))
	group.PUT("/privacy", adminSystem.UpdatePrivacyPolicyConfigHandler(serverCtx))
	group.GET("/register_config", adminSystem.GetRegisterConfigHandler(serverCtx))
	group.PUT("/register_config", adminSystem.UpdateRegisterConfigHandler(serverCtx))
	group.POST("/set_node_multiplier", adminSystem.SetNodeMultiplierHandler(serverCtx))
	group.POST("/setting_telegram_bot", adminSystem.SettingTelegramBotHandler(serverCtx))
	group.GET("/site_config", adminSystem.GetSiteConfigHandler(serverCtx))
	group.PUT("/site_config", adminSystem.UpdateSiteConfigHandler(serverCtx))
	group.GET("/subscribe_config", adminSystem.GetSubscribeConfigHandler(serverCtx))
	group.PUT("/subscribe_config", adminSystem.UpdateSubscribeConfigHandler(serverCtx))
	group.GET("/tos_config", adminSystem.GetTosConfigHandler(serverCtx))
	group.PUT("/tos_config", adminSystem.UpdateTosConfigHandler(serverCtx))
	group.GET("/verify_code_config", adminSystem.GetVerifyCodeConfigHandler(serverCtx))
	group.PUT("/verify_code_config", adminSystem.UpdateVerifyCodeConfigHandler(serverCtx))
	group.GET("/verify_config", adminSystem.GetVerifyConfigHandler(serverCtx))
	group.PUT("/verify_config", adminSystem.UpdateVerifyConfigHandler(serverCtx))
}

func registerAdminTicketRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/ticket")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.PUT("/", adminTicket.UpdateTicketStatusHandler(serverCtx))
	group.GET("/detail", adminTicket.GetTicketHandler(serverCtx))
	group.POST("/follow", adminTicket.CreateTicketFollowHandler(serverCtx))
	group.GET("/list", adminTicket.GetTicketListHandler(serverCtx))
}

func registerAdminToolRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v1/admin/tool")
	group.Use(middleware.AuthMiddleware(serverCtx))
	group.GET("/ip/location", adminTool.QueryIPLocationHandler(serverCtx))
	group.GET("/log", adminTool.GetSystemLogHandler(serverCtx))
	group.GET("/restart", adminTool.RestartSystemHandler(serverCtx))
	group.GET("/version", adminTool.GetVersionHandler(serverCtx))
}
