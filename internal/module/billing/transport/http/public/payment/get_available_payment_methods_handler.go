// Package payment holds the HTTP handler listing the payment methods a
// signed-in user can pay with.
package payment

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
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetAvailablePaymentMethodsResponse}
// @Router /v1/public/payment/methods [get]
func GetAvailablePaymentMethodsHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetAvailablePaymentMethods(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
