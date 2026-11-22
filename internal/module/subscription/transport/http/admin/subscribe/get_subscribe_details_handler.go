package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetSubscribeDetailsHandler documents Get subscribe details.
//
// @Summary Get subscribe details
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetSubscribeDetailsRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.Subscribe}
// @Router /v1/admin/subscribe/details [get]
func GetSubscribeDetailsHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetSubscribeDetailsRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetSubscribeDetails(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
