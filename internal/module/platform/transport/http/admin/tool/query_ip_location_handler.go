package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryIPLocationHandler documents Query IP Location.
//
// @Summary Query IP Location
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryIPLocationRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryIPLocationResponse}
// @Router /v1/admin/tool/ip/location [get]
func QueryIPLocationHandler(service platform.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.QueryIPLocationRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.QueryIPLocation(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
