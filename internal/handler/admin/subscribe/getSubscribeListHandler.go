package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetSubscribeListHandler documents Get subscribe list.
//
// @Summary Get subscribe list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetSubscribeListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetSubscribeListResponse}
// @Router /v1/admin/subscribe/list [get]
func GetSubscribeListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetSubscribeListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Subscription.GetSubscribeList(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
