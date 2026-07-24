package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryQuotaTaskPreCountHandler documents Query quota task pre-count.
//
// @Summary Query quota task pre-count
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.QueryQuotaTaskPreCountRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryQuotaTaskPreCountResponse}
// @Router /v1/admin/marketing/quota/pre-count [post]
func QueryQuotaTaskPreCountHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryQuotaTaskPreCountRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Support.QueryQuotaTaskPreCount(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
