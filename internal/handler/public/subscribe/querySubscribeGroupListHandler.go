package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get subscribe group list
func QuerySubscribeGroupListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := svcCtx.Subscription.QuerySubscribeGroupList(c)
		result.HttpResult(ctx, resp, err)
	}
}
