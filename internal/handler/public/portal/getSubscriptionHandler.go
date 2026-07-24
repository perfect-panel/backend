package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetSubscriptionHandler documents Get Subscription.
//
// @Summary Get Subscription
// @Tags user
// @Accept json
// @Produce json
// @Param request query dto.GetSubscriptionRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetSubscriptionResponse}
// @Router /v1/public/portal/subscribe [get]
func GetSubscriptionHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetSubscriptionRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.GetPortalSubscription(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
