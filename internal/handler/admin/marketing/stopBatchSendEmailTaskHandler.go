package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// StopBatchSendEmailTaskHandler documents Stop a batch send email task.
//
// @Summary Stop a batch send email task
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.StopBatchSendEmailTaskRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/admin/marketing/email/batch/stop [post]
func StopBatchSendEmailTaskHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.StopBatchSendEmailTaskRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		err := svcCtx.Support.StopBatchSendEmailTask(c, &req)
		result.HttpResult(ctx, nil, err)
	}
}
