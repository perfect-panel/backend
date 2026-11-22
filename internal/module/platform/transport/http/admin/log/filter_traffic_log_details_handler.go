package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterTrafficLogDetailsHandler documents Filter traffic log details.
//
// @Summary Filter traffic log details
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterTrafficLogDetailsRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterTrafficLogDetailsResponse}
// @Router /v1/admin/log/traffic/details [get]
func FilterTrafficLogDetailsHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterTrafficLogDetailsRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.FilterTrafficLogDetails(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
