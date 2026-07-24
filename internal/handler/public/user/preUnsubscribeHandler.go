package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// PreUnsubscribeHandler documents Pre Unsubscribe.
//
// @Summary Pre Unsubscribe
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.PreUnsubscribeRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.PreUnsubscribeResponse}
// @Router /v1/public/user/unsubscribe/pre [post]
func PreUnsubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PreUnsubscribeRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Subscription.PreUnsubscribe(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
