package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// PreCreateOrderHandler documents Pre create order.
//
// @Summary Pre create order
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.PurchaseOrderRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.PreOrderResponse}
// @Router /v1/public/order/pre [post]
func PreCreateOrderHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
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

		resp, err := svcCtx.Billing.PreCreateOrder(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
