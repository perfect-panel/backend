package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetUserLoginLogsHandler documents Get user login logs.
//
// @Summary Get user login logs
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetUserLoginLogsRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetUserLoginLogsResponse}
// @Router /v1/admin/user/login/logs [get]
func GetUserLoginLogsHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetUserLoginLogsRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetUserLoginLogs(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
