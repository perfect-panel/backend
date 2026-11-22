package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// BindOAuthCallbackHandler documents Bind OAuth Callback.
//
// @Summary Bind OAuth Callback
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.BindOAuthCallbackRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/public/user/bind_oauth/callback [post]
func BindOAuthCallbackHandler(service identity.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.BindOAuthCallbackRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.BindOAuthCallback(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
