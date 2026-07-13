package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Reset all subscribe tokens
func ResetAllSubscribeTokenHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := subscribe.NewResetAllSubscribeTokenLogic(c, svcCtx)
		resp, err := l.ResetAllSubscribeToken()
		result.HttpResult(ctx, resp, err)
	}
}
