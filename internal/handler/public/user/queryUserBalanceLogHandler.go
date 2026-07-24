package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserBalanceLogHandler documents Query User Balance Log.
//
// @Summary Query User Balance Log
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserBalanceLogListResponse}
// @Router /v1/public/user/balance_log [get]
func QueryUserBalanceLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := svcCtx.Billing.QueryUserBalanceLog(c)
		result.HttpResult(ctx, resp, err)
	}
}
