package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserSubscribeHandler documents Query User Subscribe.
//
// @Summary Query User Subscribe
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserSubscribeListResponse}
// @Router /v1/public/user/subscribe [get]
func QueryUserSubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := svcCtx.Subscription.QueryUserSubscribe(c)
		result.HttpResult(ctx, resp, err)
	}
}
