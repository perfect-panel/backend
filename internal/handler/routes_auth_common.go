package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	auth "github.com/perfect-panel/server/internal/handler/auth"
	authOauth "github.com/perfect-panel/server/internal/handler/auth/oauth"
	common "github.com/perfect-panel/server/internal/handler/common"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
)

func registerAuthCommonRoutes(router *server.Hertz, serverCtx *svc.ServiceContext) {
	authGroupRouter := router.Group("/v1/auth")
	authGroupRouter.Use(middleware.DeviceMiddleware(serverCtx))
	{
		authGroupRouter.GET("/check", auth.CheckUserHandler(serverCtx))
		authGroupRouter.GET("/check/telephone", auth.CheckUserTelephoneHandler(serverCtx))
		authGroupRouter.POST("/login", auth.UserLoginHandler(serverCtx))
		authGroupRouter.POST("/login/device", auth.DeviceLoginHandler(serverCtx))
		authGroupRouter.POST("/login/telephone", auth.TelephoneLoginHandler(serverCtx))
		authGroupRouter.POST("/register", auth.UserRegisterHandler(serverCtx))
		authGroupRouter.POST("/register/telephone", auth.TelephoneUserRegisterHandler(serverCtx))
		authGroupRouter.POST("/reset", auth.ResetPasswordHandler(serverCtx))
		authGroupRouter.POST("/reset/telephone", auth.TelephoneResetPasswordHandler(serverCtx))
	}

	authOauthGroupRouter := router.Group("/v1/auth/oauth")
	{
		authOauthGroupRouter.POST("/callback/apple", authOauth.AppleLoginCallbackHandler(serverCtx))
		authOauthGroupRouter.POST("/login", authOauth.OAuthLoginHandler(serverCtx))
		authOauthGroupRouter.POST("/login/token", authOauth.OAuthLoginGetTokenHandler(serverCtx))
	}

	commonGroupRouter := router.Group("/v1/common")
	commonGroupRouter.Use(middleware.DeviceMiddleware(serverCtx))
	{
		commonGroupRouter.GET("/ads", common.GetAdsHandler(serverCtx))
		commonGroupRouter.POST("/check_verification_code", common.CheckVerificationCodeHandler(serverCtx))
		commonGroupRouter.GET("/client", common.GetClientHandler(serverCtx))
		commonGroupRouter.GET("/heartbeat", common.HeartbeatHandler(serverCtx))
		commonGroupRouter.POST("/send_code", common.SendEmailCodeHandler(serverCtx))
		commonGroupRouter.POST("/send_sms_code", common.SendSmsCodeHandler(serverCtx))
		commonGroupRouter.GET("/site/config", common.GetGlobalConfigHandler(serverCtx))
		commonGroupRouter.GET("/site/privacy", common.GetPrivacyPolicyHandler(serverCtx))
		commonGroupRouter.GET("/site/stat", common.GetStatHandler(serverCtx))
		commonGroupRouter.GET("/site/tos", common.GetTosHandler(serverCtx))
	}
}
