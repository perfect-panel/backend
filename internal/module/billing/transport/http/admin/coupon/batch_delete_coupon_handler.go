// Package coupon holds the HTTP handlers of the admin coupon API; they adapt
// requests to the billing facade.
package coupon

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// BatchDeleteCouponHandler documents Batch delete coupon.
//
// @Summary Batch delete coupon
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.BatchDeleteCouponRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/coupon/batch [delete]
func BatchDeleteCouponHandler(service billing.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.BatchDeleteCouponRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.BatchDeleteCoupon(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
