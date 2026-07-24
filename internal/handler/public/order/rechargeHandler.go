package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// RechargeHandler documents Recharge.
//
// @Summary Recharge
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.RechargeOrderRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.RechargeOrderResponse}
// @Router /v1/public/order/recharge [post]
func RechargeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.RechargeOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.Recharge(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
