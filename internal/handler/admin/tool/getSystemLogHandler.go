package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/tool"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get System Log
func GetSystemLogHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := tool.NewGetSystemLogLogic(ctx, svcCtx)
		resp, err := l.GetSystemLog()
		result.HttpResult(c, resp, err)
	}
}
