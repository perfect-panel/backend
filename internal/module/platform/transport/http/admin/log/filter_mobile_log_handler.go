package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterMobileLogHandler documents Filter mobile log.
//
// @Summary Filter mobile log
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterLogParams false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterMobileLogResponse}
// @Router /v1/admin/log/mobile/list [get]
func FilterMobileLogHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterLogParams
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.FilterMobileLog(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
