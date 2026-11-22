package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetSubscriptionHandler documents Get Subscription.
//
// @Summary Get Subscription
// @Tags user
// @Accept json
// @Produce json
// @Param request query dto.GetSubscriptionRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetSubscriptionResponse}
// @Router /v1/public/portal/subscribe [get]
func GetSubscriptionHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetSubscriptionRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetPortalSubscription(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
