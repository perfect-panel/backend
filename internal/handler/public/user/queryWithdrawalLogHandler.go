package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryWithdrawalLogHandler documents Query Withdrawal Log.
//
// @Summary Query Withdrawal Log
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryWithdrawalLogListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryWithdrawalLogListResponse}
// @Router /v1/public/user/withdrawal_log [get]
func QueryWithdrawalLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryWithdrawalLogListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.QueryWithdrawalLog(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
