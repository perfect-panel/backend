package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// PreUnsubscribeHandler documents Pre Unsubscribe.
//
// @Summary Pre Unsubscribe
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.PreUnsubscribeRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PreUnsubscribeResponse}
// @Router /v1/public/user/unsubscribe/pre [post]
func PreUnsubscribeHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PreUnsubscribeRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.PreUnsubscribe(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
