package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetLoginLogHandler documents Get Login Log.
//
// @Summary Get Login Log
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetLoginLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetLoginLogResponse}
// @Router /v1/public/user/login_log [get]
func GetLoginLogHandler(service identity.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetLoginLogRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetLoginLog(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
