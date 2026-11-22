package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	auth "github.com/perfect-panel/backend/internal/module/identity/transport/http/auth"
	authOauth "github.com/perfect-panel/backend/internal/module/identity/transport/http/auth/oauth"
)

func registerAuthRoutes(router *server.Hertz, deps Dependencies) {
	authGroupRouter := router.Group("/v1/auth")
	authGroupRouter.Use(deps.deviceMiddleware())
	{
		authGroupRouter.GET("/check", auth.CheckUserHandler(deps.Identity))
		authGroupRouter.GET("/check/telephone", auth.CheckUserTelephoneHandler(deps.Identity))
		authGroupRouter.POST("/login", auth.UserLoginHandler(deps.Identity))
		authGroupRouter.POST("/login/device", auth.DeviceLoginHandler(deps.Identity))
		authGroupRouter.POST("/login/telephone", auth.TelephoneLoginHandler(deps.Identity))
		authGroupRouter.POST("/register", auth.UserRegisterHandler(deps.Identity))
		authGroupRouter.POST("/register/telephone", auth.TelephoneUserRegisterHandler(deps.Identity))
		authGroupRouter.POST("/reset", auth.ResetPasswordHandler(deps.Identity))
		authGroupRouter.POST("/reset/telephone", auth.TelephoneResetPasswordHandler(deps.Identity))
	}

	// The device transport applies here as on the other sign-in routes: a
	// device declaring Login-Type: device gets its OAuth round trip in the
	// signed envelope, while a browser, which sends no such header, is
	// untouched (docs/design/device-authentication.md).
	authOauthGroupRouter := router.Group("/v1/auth/oauth")
	authOauthGroupRouter.Use(deps.deviceMiddleware())
	{
		authOauthGroupRouter.POST("/callback/apple", authOauth.AppleLoginCallbackHandler(deps.Identity))
		authOauthGroupRouter.POST("/login", authOauth.OAuthLoginHandler(deps.Identity))
		authOauthGroupRouter.POST("/login/token", authOauth.OAuthLoginGetTokenHandler(deps.Identity))
	}
}
