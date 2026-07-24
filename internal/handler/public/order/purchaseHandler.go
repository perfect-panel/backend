package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// PurchaseHandler documents purchase Subscription.
//
// @Summary purchase Subscription
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.PurchaseOrderRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.PurchaseOrderResponse}
// @Router /v1/public/order/purchase [post]
func PurchaseHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PurchaseOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.Purchase(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
