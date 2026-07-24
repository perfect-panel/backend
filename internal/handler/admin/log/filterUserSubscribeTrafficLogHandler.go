package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// FilterUserSubscribeTrafficLogHandler documents Filter user subscribe traffic log.
//
// @Summary Filter user subscribe traffic log
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterSubscribeTrafficRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.FilterSubscribeTrafficResponse}
// @Router /v1/admin/log/subscribe/traffic/list [get]
func FilterUserSubscribeTrafficLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterSubscribeTrafficRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Platform.FilterUserSubscribeTrafficLog(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
