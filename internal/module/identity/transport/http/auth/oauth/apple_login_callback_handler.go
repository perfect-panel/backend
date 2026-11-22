package oauth

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// AppleLoginCallbackService is the part of the identity facade
// AppleLoginCallbackHandler calls.
type AppleLoginCallbackService interface {
	AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*identity.AppleLoginRedirect, error)
}

var _ AppleLoginCallbackService = identity.Service(nil)

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
func AppleLoginCallbackHandler(service AppleLoginCallbackService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.AppleLoginCallbackRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request data"})
			return
		}
		redirect, err := service.AppleLoginCallback(c, &req)
		if err != nil {
			httpx.HttpResult(ctx, nil, err)
			return
		}
		ctx.Redirect(redirect.StatusCode, []byte(redirect.Location))
	}
}
