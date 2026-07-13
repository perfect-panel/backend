package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/console"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Query server total data
func QueryServerTotalDataHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := console.NewQueryServerTotalDataLogic(ctx, svcCtx)
		resp, err := l.QueryServerTotalData()
		result.HttpResult(c, resp, err)
	}
}
