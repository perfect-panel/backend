package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// CommissionWithdrawHandler documents Commission Withdraw.
//
// @Summary Commission Withdraw
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CommissionWithdrawRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.WithdrawalLog}
// @Router /v1/public/user/commission_withdraw [post]
func CommissionWithdrawHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.CommissionWithdrawRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.CommissionWithdraw(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
