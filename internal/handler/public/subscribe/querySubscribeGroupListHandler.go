package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get subscribe group list
func QuerySubscribeGroupListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := subscribe.NewQuerySubscribeGroupListLogic(c, svcCtx)
		resp, err := l.QuerySubscribeGroupList()
		result.HttpResult(ctx, resp, err)
	}
}
