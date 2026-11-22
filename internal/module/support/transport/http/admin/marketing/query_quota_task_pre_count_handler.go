package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryQuotaTaskPreCountHandler documents Query quota task pre-count.
//
// @Summary Query quota task pre-count
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.QueryQuotaTaskPreCountRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryQuotaTaskPreCountResponse}
// @Router /v1/admin/marketing/quota/pre-count [post]
func QueryQuotaTaskPreCountHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryQuotaTaskPreCountRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryQuotaTaskPreCount(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
