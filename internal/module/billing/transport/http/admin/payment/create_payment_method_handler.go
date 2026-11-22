// Package payment holds the HTTP handlers of the admin payment method API;
// they adapt requests to the billing facade.
package payment

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CreatePaymentMethodHandler documents Create Payment Method.
//
// @Summary Create Payment Method
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreatePaymentMethodRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PaymentConfig}
// @Router /v1/admin/payment/ [post]
func CreatePaymentMethodHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.CreatePaymentMethodRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.CreatePaymentMethod(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
