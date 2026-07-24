package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// FilterTrafficLogDetailsHandler documents Filter traffic log details.
//
// @Summary Filter traffic log details
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterTrafficLogDetailsRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.FilterTrafficLogDetailsResponse}
// @Router /v1/admin/log/traffic/details [get]
func FilterTrafficLogDetailsHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterTrafficLogDetailsRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Platform.FilterTrafficLogDetails(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
