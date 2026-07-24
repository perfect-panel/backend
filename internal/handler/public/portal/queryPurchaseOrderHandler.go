package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryPurchaseOrderHandler documents Query Purchase Order.
//
// @Summary Query Purchase Order
// @Tags user
// @Accept json
// @Produce json
// @Param request query dto.QueryPurchaseOrderRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryPurchaseOrderResponse}
// @Router /v1/public/portal/order/status [get]
func QueryPurchaseOrderHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryPurchaseOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.QueryPurchaseOrder(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
