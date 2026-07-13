package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	adminSystem "github.com/perfect-panel/server/internal/handler/admin/system"
	adminTicket "github.com/perfect-panel/server/internal/handler/admin/ticket"
	adminTool "github.com/perfect-panel/server/internal/handler/admin/tool"
	adminUser "github.com/perfect-panel/server/internal/handler/admin/user"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAdminManagementRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	adminSystemGroupRouter := router.Group("/v1/admin/system")
	adminSystemGroupRouter.Use(middleware.AuthMiddleware(serverCtx))
	{
		adminSystemGroupRouter.GET("/currency_config", adminSystem.GetCurrencyConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/currency_config", adminSystem.UpdateCurrencyConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/get_node_multiplier", adminSystem.GetNodeMultiplierHandler(serverCtx))
		adminSystemGroupRouter.GET("/invite_config", adminSystem.GetInviteConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/invite_config", adminSystem.UpdateInviteConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/module", adminSystem.GetModuleConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/node_config", adminSystem.GetNodeConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/node_config", adminSystem.UpdateNodeConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/node_multiplier/preview", adminSystem.PreViewNodeMultiplierHandler(serverCtx))
		adminSystemGroupRouter.GET("/privacy", adminSystem.GetPrivacyPolicyConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/privacy", adminSystem.UpdatePrivacyPolicyConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/register_config", adminSystem.GetRegisterConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/register_config", adminSystem.UpdateRegisterConfigHandler(serverCtx))
		adminSystemGroupRouter.POST("/set_node_multiplier", adminSystem.SetNodeMultiplierHandler(serverCtx))
		adminSystemGroupRouter.POST("/setting_telegram_bot", adminSystem.SettingTelegramBotHandler(serverCtx))
		adminSystemGroupRouter.GET("/site_config", adminSystem.GetSiteConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/site_config", adminSystem.UpdateSiteConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/subscribe_config", adminSystem.GetSubscribeConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/subscribe_config", adminSystem.UpdateSubscribeConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/tos_config", adminSystem.GetTosConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/tos_config", adminSystem.UpdateTosConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/verify_code_config", adminSystem.GetVerifyCodeConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/verify_code_config", adminSystem.UpdateVerifyCodeConfigHandler(serverCtx))
		adminSystemGroupRouter.GET("/verify_config", adminSystem.GetVerifyConfigHandler(serverCtx))
		adminSystemGroupRouter.PUT("/verify_config", adminSystem.UpdateVerifyConfigHandler(serverCtx))
	}

	adminTicketGroupRouter := router.Group("/v1/admin/ticket")
	adminTicketGroupRouter.Use(middleware.AuthMiddleware(serverCtx))
	{
		adminTicketGroupRouter.PUT("/", adminTicket.UpdateTicketStatusHandler(serverCtx))
		adminTicketGroupRouter.GET("/detail", adminTicket.GetTicketHandler(serverCtx))
		adminTicketGroupRouter.POST("/follow", adminTicket.CreateTicketFollowHandler(serverCtx))
		adminTicketGroupRouter.GET("/list", adminTicket.GetTicketListHandler(serverCtx))
	}

	adminToolGroupRouter := router.Group("/v1/admin/tool")
	adminToolGroupRouter.Use(middleware.AuthMiddleware(serverCtx))
	{
		adminToolGroupRouter.GET("/ip/location", adminTool.QueryIPLocationHandler(serverCtx))
		adminToolGroupRouter.GET("/log", adminTool.GetSystemLogHandler(serverCtx))
		adminToolGroupRouter.GET("/restart", adminTool.RestartSystemHandler(serverCtx))
		adminToolGroupRouter.GET("/version", adminTool.GetVersionHandler(serverCtx))
	}

	adminUserGroupRouter := router.Group("/v1/admin/user")
	adminUserGroupRouter.Use(middleware.AuthMiddleware(serverCtx))
	{
		adminUserGroupRouter.DELETE("/", adminUser.DeleteUserHandler(serverCtx))
		adminUserGroupRouter.POST("/", adminUser.CreateUserHandler(serverCtx))
		adminUserGroupRouter.POST("/auth_method", adminUser.CreateUserAuthMethodHandler(serverCtx))
		adminUserGroupRouter.DELETE("/auth_method", adminUser.DeleteUserAuthMethodHandler(serverCtx))
		adminUserGroupRouter.PUT("/auth_method", adminUser.UpdateUserAuthMethodHandler(serverCtx))
		adminUserGroupRouter.GET("/auth_method", adminUser.GetUserAuthMethodHandler(serverCtx))
		adminUserGroupRouter.PUT("/basic", adminUser.UpdateUserBasicInfoHandler(serverCtx))
		adminUserGroupRouter.DELETE("/batch", adminUser.BatchDeleteUserHandler(serverCtx))
		adminUserGroupRouter.GET("/current", adminUser.CurrentUserHandler(serverCtx))
		adminUserGroupRouter.GET("/detail", adminUser.GetUserDetailHandler(serverCtx))
		adminUserGroupRouter.PUT("/device", adminUser.UpdateUserDeviceHandler(serverCtx))
		adminUserGroupRouter.DELETE("/device", adminUser.DeleteUserDeviceHandler(serverCtx))
		adminUserGroupRouter.PUT("/device/kick_offline", adminUser.KickOfflineByUserDeviceHandler(serverCtx))
		adminUserGroupRouter.GET("/list", adminUser.GetUserListHandler(serverCtx))
		adminUserGroupRouter.GET("/login/logs", adminUser.GetUserLoginLogsHandler(serverCtx))
		adminUserGroupRouter.PUT("/notify", adminUser.UpdateUserNotifySettingHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe", adminUser.GetUserSubscribeHandler(serverCtx))
		adminUserGroupRouter.POST("/subscribe", adminUser.CreateUserSubscribeHandler(serverCtx))
		adminUserGroupRouter.PUT("/subscribe", adminUser.UpdateUserSubscribeHandler(serverCtx))
		adminUserGroupRouter.DELETE("/subscribe", adminUser.DeleteUserSubscribeHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe/detail", adminUser.GetUserSubscribeByIdHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe/device", adminUser.GetUserSubscribeDevicesHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe/logs", adminUser.GetUserSubscribeLogsHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe/reset/logs", adminUser.GetUserSubscribeResetTrafficLogsHandler(serverCtx))
		adminUserGroupRouter.POST("/subscribe/reset/token", adminUser.ResetUserSubscribeTokenHandler(serverCtx))
		adminUserGroupRouter.POST("/subscribe/reset/traffic", adminUser.ResetUserSubscribeTrafficHandler(serverCtx))
		adminUserGroupRouter.POST("/subscribe/toggle", adminUser.ToggleUserSubscribeStatusHandler(serverCtx))
		adminUserGroupRouter.GET("/subscribe/traffic_logs", adminUser.GetUserSubscribeTrafficLogsHandler(serverCtx))
	}
}
