package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterGiftLogHandler documents Filter gift log.
//
// @Summary Filter gift log
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterGiftLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterGiftLogResponse}
// @Router /v1/admin/log/gift/list [get]
func FilterGiftLogHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterGiftLogRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.FilterGiftLog(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
