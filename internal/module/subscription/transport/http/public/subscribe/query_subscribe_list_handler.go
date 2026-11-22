package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QuerySubscribeListHandler documents Get subscribe list.
//
// @Summary Get subscribe list
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QuerySubscribeListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QuerySubscribeListResponse}
// @Router /v1/public/subscribe/list [get]
func QuerySubscribeListHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QuerySubscribeListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QuerySubscribeList(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
