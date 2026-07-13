package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get subscribe group list
func GetSubscribeGroupListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := subscribe.NewGetSubscribeGroupListLogic(c, svcCtx)
		resp, err := l.GetSubscribeGroupList()
		result.HttpResult(ctx, resp, err)
	}
}
