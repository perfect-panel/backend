package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// FilterMobileLogHandler documents Filter mobile log.
//
// @Summary Filter mobile log
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.FilterLogParams false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.FilterMobileLogResponse}
// @Router /v1/admin/log/mobile/list [get]
func FilterMobileLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.FilterLogParams
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Platform.FilterMobileLog(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
