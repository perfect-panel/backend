package oauth

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth/oauth"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// AppleLoginCallbackHandler documents Apple Login Callback.
//
// @Summary Apple Login Callback
// @Tags common
// @Accept x-www-form-urlencoded
// @Produce json
// @Param code formData string true "Authorization code"
// @Param id_token formData string false "Apple identity token"
// @Param state formData string false "OAuth state"
// @Success 302 {string} string "Redirect to the configured frontend"
// @Router /v1/auth/oauth/callback/apple [post]
func AppleLoginCallbackHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.AppleLoginCallbackRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request data"})
			return
		}
		l := oauth.NewAppleLoginCallbackLogic(c, oauth.AppleLoginCallbackDependencies{
			Redis:            svcCtx.Redis,
			FallbackRedirect: svcCtx.Config.Site.Host,
		})
		redirect, err := l.AppleLoginCallback(&req)
		if err != nil {
			result.HttpResult(ctx, nil, err)
			return
		}
		ctx.Redirect(redirect.StatusCode, []byte(redirect.Location))
	}
}
