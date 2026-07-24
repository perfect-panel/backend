package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetBatchSendEmailTaskListHandler documents Get batch send email task list.
//
// @Summary Get batch send email task list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetBatchSendEmailTaskListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetBatchSendEmailTaskListResponse}
// @Router /v1/admin/marketing/email/batch/list [get]
func GetBatchSendEmailTaskListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetBatchSendEmailTaskListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Support.GetBatchSendEmailTaskList(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
