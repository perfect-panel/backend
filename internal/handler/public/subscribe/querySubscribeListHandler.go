package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QuerySubscribeListHandler documents Get subscribe list.
//
// @Summary Get subscribe list
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QuerySubscribeListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QuerySubscribeListResponse}
// @Router /v1/public/subscribe/list [get]
func QuerySubscribeListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QuerySubscribeListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Subscription.QuerySubscribeList(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
