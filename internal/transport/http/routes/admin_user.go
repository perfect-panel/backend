package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	identityAdminUser "github.com/perfect-panel/backend/internal/module/identity/transport/http/admin/user"
	subscriptionAdminUser "github.com/perfect-panel/backend/internal/module/subscription/transport/http/admin/user"
)

func registerAdminUserRoutes(router *server.Hertz, deps Dependencies) {
	adminUserGroupRouter := deps.adminGroup(router, "/v1/admin/user")
	{
		adminUserGroupRouter.DELETE("/", identityAdminUser.DeleteUserHandler(deps.Identity))
		adminUserGroupRouter.POST("/", identityAdminUser.CreateUserHandler(deps.Identity))
		adminUserGroupRouter.POST("/auth_method", identityAdminUser.CreateUserAuthMethodHandler(deps.Identity))
		adminUserGroupRouter.DELETE("/auth_method", identityAdminUser.DeleteUserAuthMethodHandler(deps.Identity))
		adminUserGroupRouter.PUT("/auth_method", identityAdminUser.UpdateUserAuthMethodHandler(deps.Identity))
		adminUserGroupRouter.GET("/auth_method", identityAdminUser.GetUserAuthMethodHandler(deps.Identity))
		adminUserGroupRouter.PUT("/basic", identityAdminUser.UpdateUserBasicInfoHandler(deps.Identity))
		adminUserGroupRouter.DELETE("/batch", identityAdminUser.BatchDeleteUserHandler(deps.Identity))
		adminUserGroupRouter.GET("/current", identityAdminUser.CurrentUserHandler(deps.Identity))
		adminUserGroupRouter.GET("/detail", identityAdminUser.GetUserDetailHandler(deps.Identity))
		adminUserGroupRouter.PUT("/device", identityAdminUser.UpdateUserDeviceHandler(deps.Identity))
		adminUserGroupRouter.DELETE("/device", identityAdminUser.DeleteUserDeviceHandler(deps.Identity))
		adminUserGroupRouter.PUT("/device/kick_offline", identityAdminUser.KickOfflineByUserDeviceHandler(deps.Identity))
		adminUserGroupRouter.GET("/list", identityAdminUser.GetUserListHandler(deps.Identity))
		adminUserGroupRouter.GET("/login/logs", identityAdminUser.GetUserLoginLogsHandler(deps.Identity))
		adminUserGroupRouter.PUT("/notify", identityAdminUser.UpdateUserNotifySettingHandler(deps.Identity))
		adminUserGroupRouter.GET("/subscribe", subscriptionAdminUser.GetUserSubscribeHandler(deps.Subscription))
		adminUserGroupRouter.POST("/subscribe", subscriptionAdminUser.CreateUserSubscribeHandler(deps.Subscription))
		adminUserGroupRouter.PUT("/subscribe", subscriptionAdminUser.UpdateUserSubscribeHandler(deps.Subscription))
		adminUserGroupRouter.DELETE("/subscribe", subscriptionAdminUser.DeleteUserSubscribeHandler(deps.Subscription))
		adminUserGroupRouter.GET("/subscribe/detail", subscriptionAdminUser.GetUserSubscribeByIdHandler(deps.Subscription))
		adminUserGroupRouter.GET("/subscribe/device", subscriptionAdminUser.GetUserSubscribeDevicesHandler(deps.Subscription))
		adminUserGroupRouter.GET("/subscribe/logs", subscriptionAdminUser.GetUserSubscribeLogsHandler(deps.Subscription))
		adminUserGroupRouter.GET("/subscribe/reset/logs", subscriptionAdminUser.GetUserSubscribeResetTrafficLogsHandler(deps.Subscription))
		adminUserGroupRouter.POST("/subscribe/reset/token", subscriptionAdminUser.ResetUserSubscribeTokenHandler(deps.Subscription))
		adminUserGroupRouter.POST("/subscribe/reset/traffic", subscriptionAdminUser.ResetUserSubscribeTrafficHandler(deps.Subscription))
		adminUserGroupRouter.POST("/subscribe/toggle", subscriptionAdminUser.ToggleUserSubscribeStatusHandler(deps.Subscription))
		adminUserGroupRouter.GET("/subscribe/traffic_logs", subscriptionAdminUser.GetUserSubscribeTrafficLogsHandler(deps.Subscription))
	}
}
