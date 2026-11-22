package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// checkoutTokenHeader carries the guest checkout capability outside the URL,
// where query strings end up in access logs, browser history and referrers.
const checkoutTokenHeader = "X-Checkout-Token"

// QueryPurchaseOrderHandler documents Query Purchase Order.
//
// @Summary Query Purchase Order
// @Tags user
// @Accept json
// @Produce json
// @Param request query dto.QueryPurchaseOrderRequest false "Request parameters"
// @Param X-Checkout-Token header string false "Guest checkout capability; preferred over the checkout_token query parameter"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryPurchaseOrderResponse}
// @Router /v1/public/portal/order/status [get]
func QueryPurchaseOrderHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryPurchaseOrderRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		if token := string(ctx.GetHeader(checkoutTokenHeader)); token != "" {
			req.CheckoutToken = token
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryPurchaseOrder(c, &req)
		if err == nil && resp != nil && resp.Token != "" {
			// The answer carries a session token; no cache may keep it.
			ctx.Header("Cache-Control", "no-store")
		}
		httpx.HttpResult(ctx, resp, err)
	}
}
