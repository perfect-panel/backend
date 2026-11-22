package routes

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	identityCommon "github.com/perfect-panel/backend/internal/module/identity/transport/http/common"
	platformCommon "github.com/perfect-panel/backend/internal/module/platform/transport/http/common"
	supportCommon "github.com/perfect-panel/backend/internal/module/support/transport/http/common"
)

func registerCommonRoutes(router *server.Hertz, deps Dependencies) {
	commonGroupRouter := router.Group("/v1/common")
	commonGroupRouter.Use(deps.deviceMiddleware())
	{
		commonGroupRouter.GET("/ads", supportCommon.GetAdsHandler(deps.Support))
		commonGroupRouter.POST("/check_verification_code", identityCommon.CheckVerificationCodeHandler(deps.Identity))
		commonGroupRouter.GET("/client", platformCommon.GetClientHandler(deps.Platform))
		commonGroupRouter.GET("/heartbeat", platformCommon.HeartbeatHandler(deps.Platform))
		commonGroupRouter.GET("/site/config", platformCommon.GetGlobalConfigHandler(deps.Platform))
		commonGroupRouter.GET("/site/privacy", platformCommon.GetPrivacyPolicyHandler(deps.Platform))
		commonGroupRouter.GET("/site/stat", platformCommon.GetStatHandler(deps.Platform))
		commonGroupRouter.GET("/site/tos", platformCommon.GetTosHandler(deps.Platform))
	}

	// A signed-in caller asks for a code to bind an address to its own
	// account, which stays possible while registration is closed; the
	// optional authentication tells the identity module who asks. It runs
	// before the device transport, as on /v1/public: the transport must see
	// the signed device session, so dropping the Login-Type header cannot
	// get a device a plaintext answer (docs/design/device-authentication.md).
	codeGroupRouter := router.Group("/v1/common")
	codeGroupRouter.Use(deps.optionalAuthMiddleware(), deps.deviceMiddleware())
	{
		codeGroupRouter.POST("/send_code", identityCommon.SendEmailCodeHandler(deps.Identity))
		codeGroupRouter.POST("/send_sms_code", identityCommon.SendSmsCodeHandler(deps.Identity))
	}
}
