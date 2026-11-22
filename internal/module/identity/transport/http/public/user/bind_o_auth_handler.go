package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// BindOAuthHandler documents Bind OAuth.
//
// @Summary Bind OAuth
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.BindOAuthRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.BindOAuthResponse}
// @Router /v1/public/user/bind_oauth [post]
func BindOAuthHandler(service identity.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.BindOAuthRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.BindOAuth(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
