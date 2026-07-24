package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// CreateQuotaTaskHandler documents Create a quota task.
//
// @Summary Create a quota task
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreateQuotaTaskRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/admin/marketing/quota/create [post]
func CreateQuotaTaskHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.CreateQuotaTaskRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		err := svcCtx.Support.CreateQuotaTask(c, &req)
		result.HttpResult(ctx, nil, err)
	}
}
