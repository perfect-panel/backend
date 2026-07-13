package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/tool"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Restart System
func RestartSystemHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := tool.NewRestartSystemLogic(ctx, svcCtx)
		err := l.RestartSystem()
		result.HttpResult(c, nil, err)
	}
}
