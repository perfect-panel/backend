package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.QueryUserSubscribeListResponse

// QueryUserSubscribeHandler documents Query User Subscribe.
//
// @Summary Query User Subscribe
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryUserSubscribeListResponse}
// @Router /v1/public/user/subscribe [get]
func QueryUserSubscribeHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryUserSubscribe(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
