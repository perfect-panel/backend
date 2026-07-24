package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserSubscribeNodeListHandler documents Get user subscribe node info.
//
// @Summary Get user subscribe node info
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserSubscribeNodeListResponse}
// @Router /v1/public/subscribe/node/list [get]
func QueryUserSubscribeNodeListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := svcCtx.Subscription.QueryUserSubscribeNodeList(c)
		result.HttpResult(ctx, resp, err)
	}
}
