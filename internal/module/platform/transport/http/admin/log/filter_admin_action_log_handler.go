package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// FilterAdminActionLogHandler documents Filter admin action log.
//
// @Summary Filter admin action log
// @Description Pages the administrators' audit trail: settings changes, marketing tasks, ticket actions and Telegram bot commands, with the acting administrator and the request they came from.
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterAdminActionLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.FilterAdminActionLogResponse}
// @Router /v1/admin/log/admin/list [get]
func FilterAdminActionLogHandler(service Logs) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterAdminActionLogRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.FilterAdminActionLog(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
