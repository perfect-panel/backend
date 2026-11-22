package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterOrderLogHandler documents Filter order creation logs.
//
// @Summary Filter order creation logs
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterOrderLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterOrderLogResponse}
// @Router /v1/admin/log/order/list [get]
func FilterOrderLogHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterOrderLogRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		if err := validation.Validate(&req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}

		resp, err := service.FilterOrderLog(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
