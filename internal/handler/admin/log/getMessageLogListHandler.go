package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetMessageLogListHandler documents Get message log list.
//
// @Summary Get message log list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetMessageLogListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetMessageLogListResponse}
// @Router /v1/admin/log/message/list [get]
func GetMessageLogListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetMessageLogListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Platform.GetMessageLogList(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
