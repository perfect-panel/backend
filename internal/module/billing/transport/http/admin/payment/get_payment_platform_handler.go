package payment

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.PaymentPlatformResponse

// GetPaymentPlatformHandler documents Get supported payment platform.
//
// @Summary Get supported payment platform
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PaymentPlatformResponse}
// @Router /v1/admin/payment/platform [get]
func GetPaymentPlatformHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetPaymentPlatform(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
