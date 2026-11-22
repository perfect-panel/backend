package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetBatchSendEmailTaskStatusHandler documents Get batch send email task status.
//
// @Summary Get batch send email task status
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.GetBatchSendEmailTaskStatusRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetBatchSendEmailTaskStatusResponse}
// @Router /v1/admin/marketing/email/batch/status [post]
func GetBatchSendEmailTaskStatusHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetBatchSendEmailTaskStatusRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetBatchSendEmailTaskStatus(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
