package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.QueryUserSubscribeNodeListResponse

// QueryUserSubscribeNodeListHandler documents Get user subscribe node info.
//
// @Summary Get user subscribe node info
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryUserSubscribeNodeListResponse}
// @Router /v1/public/subscribe/node/list [get]
func QueryUserSubscribeNodeListHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryUserSubscribeNodeList(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
