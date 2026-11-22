package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	billingPublicUser "github.com/perfect-panel/backend/internal/module/billing/transport/http/public/user"
	identityPublicUser "github.com/perfect-panel/backend/internal/module/identity/transport/http/public/user"
	subscriptionPublicUser "github.com/perfect-panel/backend/internal/module/subscription/transport/http/public/user"
)

func registerPublicUserRoutes(router *server.Hertz, deps Dependencies) {
	publicUserGroupRouter := router.Group("/v1/public/user")
	publicUserGroupRouter.Use(deps.authMiddleware(), deps.deviceMiddleware())
	publicUserGroupRouter.GET("/affiliate/count", billingPublicUser.QueryUserAffiliateHandler(deps.Billing))
	publicUserGroupRouter.GET("/affiliate/list", billingPublicUser.QueryUserAffiliateListHandler(deps.Billing))
	publicUserGroupRouter.GET("/balance_log", billingPublicUser.QueryUserBalanceLogHandler(deps.Billing))
	publicUserGroupRouter.PUT("/bind_email", identityPublicUser.UpdateBindEmailHandler(deps.Identity))
	publicUserGroupRouter.PUT("/bind_mobile", identityPublicUser.UpdateBindMobileHandler(deps.Identity))
	publicUserGroupRouter.POST("/bind_oauth", identityPublicUser.BindOAuthHandler(deps.Identity))
	publicUserGroupRouter.POST("/bind_oauth/callback", identityPublicUser.BindOAuthCallbackHandler(deps.Identity))
	publicUserGroupRouter.GET("/bind_telegram", identityPublicUser.BindTelegramHandler(deps.Identity))
	publicUserGroupRouter.GET("/commission_log", billingPublicUser.QueryUserCommissionLogHandler(deps.Billing))
	publicUserGroupRouter.POST("/commission_withdraw", billingPublicUser.CommissionWithdrawHandler(deps.Billing))
	publicUserGroupRouter.GET("/devices", identityPublicUser.GetDeviceListHandler(deps.Identity))
	publicUserGroupRouter.GET("/info", identityPublicUser.QueryUserInfoHandler(deps.Identity))
	publicUserGroupRouter.GET("/login_log", identityPublicUser.GetLoginLogHandler(deps.Identity))
	publicUserGroupRouter.POST("/logout", identityPublicUser.LogoutHandler(deps.Identity))
	publicUserGroupRouter.PUT("/notify", identityPublicUser.UpdateUserNotifyHandler(deps.Identity))
	publicUserGroupRouter.GET("/oauth_methods", identityPublicUser.GetOAuthMethodsHandler(deps.Identity))
	publicUserGroupRouter.PUT("/password", identityPublicUser.UpdateUserPasswordHandler(deps.Identity))
	publicUserGroupRouter.PUT("/rules", identityPublicUser.UpdateUserRulesHandler(deps.Identity))
	publicUserGroupRouter.GET("/subscribe", subscriptionPublicUser.QueryUserSubscribeHandler(deps.Subscription))
	publicUserGroupRouter.GET("/subscribe_log", subscriptionPublicUser.GetSubscribeLogHandler(deps.Subscription))
	publicUserGroupRouter.PUT("/subscribe_note", subscriptionPublicUser.UpdateUserSubscribeNoteHandler(deps.Subscription))
	publicUserGroupRouter.PUT("/subscribe_token", subscriptionPublicUser.ResetUserSubscribeTokenHandler(deps.Subscription))
	publicUserGroupRouter.PUT("/unbind_device", identityPublicUser.UnbindDeviceHandler(deps.Identity))
	publicUserGroupRouter.POST("/unbind_oauth", identityPublicUser.UnbindOAuthHandler(deps.Identity))
	publicUserGroupRouter.POST("/unbind_telegram", identityPublicUser.UnbindTelegramHandler(deps.Identity))
	publicUserGroupRouter.POST("/unsubscribe", subscriptionPublicUser.UnsubscribeHandler(deps.Subscription))
	publicUserGroupRouter.POST("/unsubscribe/pre", subscriptionPublicUser.PreUnsubscribeHandler(deps.Subscription))
	publicUserGroupRouter.POST("/verify_email", identityPublicUser.VerifyEmailHandler(deps.Identity))
	publicUserGroupRouter.GET("/withdrawal_log", billingPublicUser.QueryWithdrawalLogHandler(deps.Billing))
}
