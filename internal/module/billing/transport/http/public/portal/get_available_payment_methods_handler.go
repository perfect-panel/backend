// Package portal holds the HTTP handlers of the guest storefront API: plans,
// payment methods, guest orders, their checkout and status; they adapt
// requests to the billing facade.
package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetAvailablePaymentMethodsResponse

// GetAvailablePaymentMethodsHandler documents Get available payment methods.
//
// @Summary Get available payment methods
// @Tags user
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetAvailablePaymentMethodsResponse}
// @Router /v1/public/portal/payment-method [get]
func GetAvailablePaymentMethodsHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetAvailablePaymentMethods(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
