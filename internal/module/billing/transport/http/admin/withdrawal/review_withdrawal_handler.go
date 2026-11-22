package withdrawal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// ReviewWithdrawalHandler documents the pending withdrawal transition.
//
// @Summary Review withdrawal
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.ReviewWithdrawalRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/withdrawal/status [put]
func ReviewWithdrawalHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.ReviewWithdrawalRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		if err := validation.Validate(&req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		err := service.ReviewWithdrawal(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
