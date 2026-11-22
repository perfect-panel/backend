package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminAuthMethod "github.com/perfect-panel/backend/internal/module/identity/transport/http/admin/authmethod"
	adminServer "github.com/perfect-panel/backend/internal/module/network/transport/http/admin/server"
	adminConsole "github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/console"
	adminLog "github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/log"
	adminSystem "github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/system"
	adminTool "github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/tool"
	adminTicket "github.com/perfect-panel/backend/internal/module/support/transport/http/admin/ticket"
)

func registerAdminAuthMethodRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/auth-method")
	group.GET("/config", adminAuthMethod.GetAuthMethodConfigHandler(deps.Identity))
	group.PUT("/config", adminAuthMethod.UpdateAuthMethodConfigHandler(deps.Identity))
	group.GET("/email_platform", adminAuthMethod.GetEmailPlatformHandler(deps.Identity))
	group.GET("/list", adminAuthMethod.GetAuthMethodListHandler(deps.Identity))
	group.GET("/sms_platform", adminAuthMethod.GetSmsPlatformHandler(deps.Identity))
	group.POST("/test_email_send", adminAuthMethod.TestEmailSendHandler(deps.Identity))
	group.POST("/test_sms_send", adminAuthMethod.TestSmsSendHandler(deps.Identity))
}

func registerAdminConsoleRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/console")
	group.GET("/revenue", adminConsole.QueryRevenueStatisticsHandler(deps.Platform))
	group.GET("/server", adminConsole.QueryServerTotalDataHandler(deps.Platform))
	group.GET("/ticket", adminConsole.QueryTicketWaitReplyHandler(deps.Platform))
	group.GET("/user", adminConsole.QueryUserStatisticsHandler(deps.Platform))
}

func registerAdminLogRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/log")
	group.GET("/admin/list", adminLog.FilterAdminActionLogHandler(deps.Platform))
	group.GET("/balance/list", adminLog.FilterBalanceLogHandler(deps.Platform))
	group.GET("/commission/list", adminLog.FilterCommissionLogHandler(deps.Platform))
	group.GET("/email/list", adminLog.FilterEmailLogHandler(deps.Platform))
	group.GET("/gift/list", adminLog.FilterGiftLogHandler(deps.Platform))
	group.GET("/login/list", adminLog.FilterLoginLogHandler(deps.Platform))
	group.GET("/message/list", adminLog.GetMessageLogListHandler(deps.Platform))
	group.GET("/mobile/list", adminLog.FilterMobileLogHandler(deps.Platform))
	group.GET("/payment/unmatched/list", adminLog.FilterUnmatchedPaymentLogHandler(deps.Platform))
	group.GET("/order/list", adminLog.FilterOrderLogHandler(deps.Platform))
	group.GET("/register/list", adminLog.FilterRegisterLogHandler(deps.Platform))
	group.GET("/server/traffic/list", adminLog.FilterServerTrafficLogHandler(deps.Platform))
	group.GET("/setting", adminLog.GetLogSettingHandler(deps.Platform))
	group.POST("/setting", adminLog.UpdateLogSettingHandler(deps.Platform))
	group.GET("/subscribe/list", adminLog.FilterSubscribeLogHandler(deps.Platform))
	group.GET("/subscribe/reset/list", adminLog.FilterResetSubscribeLogHandler(deps.Platform))
	group.GET("/subscribe/traffic/list", adminLog.FilterUserSubscribeTrafficLogHandler(deps.Platform))
	group.GET("/traffic/details", adminLog.FilterTrafficLogDetailsHandler(deps.Platform))
}

func registerAdminServerRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/server")
	group.POST("/create", adminServer.CreateServerHandler(deps.Network))
	group.POST("/delete", adminServer.DeleteServerHandler(deps.Network))
	group.GET("/list", adminServer.FilterServerListHandler(deps.Network))
	group.POST("/node/create", adminServer.CreateNodeHandler(deps.Network))
	group.POST("/node/delete", adminServer.DeleteNodeHandler(deps.Network))
	group.GET("/node/list", adminServer.FilterNodeListHandler(deps.Network))
	group.POST("/node/sort", adminServer.ResetSortWithNodeHandler(deps.Network))
	group.POST("/node/status/toggle", adminServer.ToggleNodeStatusHandler(deps.Network))
	group.GET("/node/tags", adminServer.QueryNodeTagHandler(deps.Network))
	group.GET("/node_config", adminServer.GetServerNodeConfigHandler(deps.Network))
	group.POST("/node_config/update", adminServer.UpdateServerNodeConfigHandler(deps.Network))
	group.POST("/node/update", adminServer.UpdateNodeHandler(deps.Network))
	group.GET("/protocols", adminServer.GetServerProtocolsHandler(deps.Network))
	group.POST("/server/sort", adminServer.ResetSortWithServerHandler(deps.Network))
	group.POST("/update", adminServer.UpdateServerHandler(deps.Network))
}

func registerAdminSystemRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/system")
	group.GET("/currency_config", adminSystem.GetCurrencyConfigHandler(deps.Platform))
	group.PUT("/currency_config", adminSystem.UpdateCurrencyConfigHandler(deps.Platform))
	group.GET("/get_node_multiplier", adminSystem.GetNodeMultiplierHandler(deps.Platform))
	group.GET("/invite_config", adminSystem.GetInviteConfigHandler(deps.Platform))
	group.PUT("/invite_config", adminSystem.UpdateInviteConfigHandler(deps.Platform))
	group.GET("/node_config", adminSystem.GetNodeConfigHandler(deps.Platform))
	group.PUT("/node_config", adminSystem.UpdateNodeConfigHandler(deps.Platform))
	group.GET("/node_multiplier/preview", adminSystem.PreViewNodeMultiplierHandler(deps.Platform))
	group.GET("/privacy", adminSystem.GetPrivacyPolicyConfigHandler(deps.Platform))
	group.PUT("/privacy", adminSystem.UpdatePrivacyPolicyConfigHandler(deps.Platform))
	group.GET("/register_config", adminSystem.GetRegisterConfigHandler(deps.Platform))
	group.PUT("/register_config", adminSystem.UpdateRegisterConfigHandler(deps.Platform))
	group.POST("/set_node_multiplier", adminSystem.SetNodeMultiplierHandler(deps.Platform))
	group.POST("/setting_telegram_bot", adminSystem.SettingTelegramBotHandler(deps.Platform))
	group.GET("/site_config", adminSystem.GetSiteConfigHandler(deps.Platform))
	group.PUT("/site_config", adminSystem.UpdateSiteConfigHandler(deps.Platform))
	group.GET("/subscribe_config", adminSystem.GetSubscribeConfigHandler(deps.Platform))
	group.PUT("/subscribe_config", adminSystem.UpdateSubscribeConfigHandler(deps.Platform))
	group.GET("/tos_config", adminSystem.GetTosConfigHandler(deps.Platform))
	group.PUT("/tos_config", adminSystem.UpdateTosConfigHandler(deps.Platform))
	group.GET("/verify_code_config", adminSystem.GetVerifyCodeConfigHandler(deps.Platform))
	group.PUT("/verify_code_config", adminSystem.UpdateVerifyCodeConfigHandler(deps.Platform))
	group.GET("/verify_config", adminSystem.GetVerifyConfigHandler(deps.Platform))
	group.PUT("/verify_config", adminSystem.UpdateVerifyConfigHandler(deps.Platform))
}

func registerAdminTicketRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/ticket")
	group.PUT("/", adminTicket.UpdateTicketStatusHandler(deps.Support))
	group.GET("/detail", adminTicket.GetTicketHandler(deps.Support))
	group.POST("/follow", adminTicket.CreateTicketFollowHandler(deps.Support))
	group.GET("/list", adminTicket.GetTicketListHandler(deps.Support))
}

func registerAdminToolRoutes(router *server.Hertz, deps Dependencies) {
	group := deps.adminGroup(router, "/v1/admin/tool")
	group.GET("/ip/location", adminTool.QueryIPLocationHandler(deps.Platform))
	group.GET("/log", adminTool.GetSystemLogHandler(deps.Platform))
	// A restart is a mutation: it takes a POST, which a browser does not
	// issue from a link or a prefetch.
	group.POST("/restart", adminTool.RestartSystemHandler(deps.Platform))
	group.GET("/version", adminTool.GetVersionHandler(deps.Platform))
}
