// Package user holds the HTTP handlers of the user wallet API: commission
// withdrawal, balance, commission and withdrawal statements, and the
// affiliate overview; they adapt requests to the billing facade.
package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CommissionWithdrawHandler documents Commission Withdraw.
//
// @Summary Commission Withdraw
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CommissionWithdrawRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.WithdrawalLog}
// @Router /v1/public/user/commission_withdraw [post]
func CommissionWithdrawHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.CommissionWithdrawRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.CommissionWithdraw(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
