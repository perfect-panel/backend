package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserCommissionLogHandler documents Query User Commission Log.
//
// @Summary Query User Commission Log
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryUserCommissionLogListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserCommissionLogListResponse}
// @Router /v1/public/user/commission_log [get]
func QueryUserCommissionLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryUserCommissionLogListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.QueryUserCommissionLog(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
