package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// PreCreateOrderHandler documents Pre create order.
//
// @Summary Pre create order
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.PurchaseOrderRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PreOrderResponse}
// @Router /v1/public/order/pre [post]
func PreCreateOrderHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PurchaseOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.PreCreateOrder(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
