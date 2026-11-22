package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// ResetUserSubscribeTokenHandler documents Reset user subscribe token.
//
// @Summary Reset user subscribe token
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.ResetUserSubscribeTokenRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/user/subscribe/reset/token [post]
func ResetUserSubscribeTokenHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.ResetUserSubscribeTokenRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.ResetUserSubscribeToken(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
