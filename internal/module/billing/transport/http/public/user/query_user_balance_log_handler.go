package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.QueryUserBalanceLogListResponse

// QueryUserBalanceLogHandler documents Query User Balance Log.
//
// @Summary Query User Balance Log
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryUserBalanceLogListResponse}
// @Router /v1/public/user/balance_log [get]
func QueryUserBalanceLogHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryUserBalanceLog(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
