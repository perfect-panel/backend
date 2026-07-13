package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/console"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Query user statistics
func QueryUserStatisticsHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := console.NewQueryUserStatisticsLogic(ctx, svcCtx)
		resp, err := l.QueryUserStatistics()
		result.HttpResult(c, resp, err)
	}
}
