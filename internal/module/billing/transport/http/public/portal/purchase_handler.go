package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// PurchaseHandler documents Purchase subscription.
//
// @Summary Purchase subscription
// @Tags user
// @Accept json
// @Produce json
// @Param request body dto.PortalPurchaseRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PortalPurchaseResponse}
// @Router /v1/public/portal/purchase [post]
func PurchaseHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PortalPurchaseRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.PortalPurchase(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
