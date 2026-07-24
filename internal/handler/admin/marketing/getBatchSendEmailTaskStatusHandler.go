package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetBatchSendEmailTaskStatusHandler documents Get batch send email task status.
//
// @Summary Get batch send email task status
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.GetBatchSendEmailTaskStatusRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetBatchSendEmailTaskStatusResponse}
// @Router /v1/admin/marketing/email/batch/status [post]
func GetBatchSendEmailTaskStatusHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetBatchSendEmailTaskStatusRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Support.GetBatchSendEmailTaskStatus(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
