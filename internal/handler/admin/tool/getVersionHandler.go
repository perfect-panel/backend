package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/tool"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// GetVersionHandler Get Version
func GetVersionHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := tool.NewGetVersionLogic(ctx, svcCtx)
		resp, err := l.GetVersion()
		result.HttpResult(c, resp, err)
	}
}
