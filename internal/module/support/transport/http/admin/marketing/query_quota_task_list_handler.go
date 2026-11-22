package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryQuotaTaskListHandler documents Query quota task list.
//
// @Summary Query quota task list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryQuotaTaskListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryQuotaTaskListResponse}
// @Router /v1/admin/marketing/quota/list [get]
func QueryQuotaTaskListHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryQuotaTaskListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryQuotaTaskList(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
