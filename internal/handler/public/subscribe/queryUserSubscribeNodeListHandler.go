package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get user subscribe node info
func QueryUserSubscribeNodeListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := subscribe.NewQueryUserSubscribeNodeListLogic(c, svcCtx)
		resp, err := l.QueryUserSubscribeNodeList()
		result.HttpResult(ctx, resp, err)
	}
}
