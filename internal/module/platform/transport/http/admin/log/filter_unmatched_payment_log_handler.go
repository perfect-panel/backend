package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterUnmatchedPaymentLogHandler documents Filter unmatched payment log.
//
// @Summary Filter unmatched payment log
// @Description Pages the payments a gateway confirmed that could not settle their order, with the order and trade numbers, the amount and the reason, for manual refunds.
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterUnmatchedPaymentLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterUnmatchedPaymentLogResponse}
// @Router /v1/admin/log/payment/unmatched/list [get]
func FilterUnmatchedPaymentLogHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterUnmatchedPaymentLogRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.FilterUnmatchedPaymentLog(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
