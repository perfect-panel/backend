package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UnbindOAuthHandler documents Unbind OAuth.
//
// @Summary Unbind OAuth
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UnbindOAuthRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/public/user/unbind_oauth [post]
func UnbindOAuthHandler(service identity.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.UnbindOAuthRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.UnbindOAuth(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
