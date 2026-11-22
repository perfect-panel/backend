package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// RechargeHandler documents Recharge.
//
// @Summary Recharge
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.RechargeOrderRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.RechargeOrderResponse}
// @Router /v1/public/order/recharge [post]
func RechargeHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.RechargeOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.Recharge(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
