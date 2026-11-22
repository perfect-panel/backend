package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// PrePurchaseOrderHandler documents Pre Purchase Order.
//
// @Summary Pre Purchase Order
// @Tags user
// @Accept json
// @Produce json
// @Param request body dto.PrePurchaseOrderRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PrePurchaseOrderResponse}
// @Router /v1/public/portal/pre [post]
func PrePurchaseOrderHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PrePurchaseOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.PortalPrePurchase(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
