package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get site config
func GetSiteConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := system.NewGetSiteConfigLogic(ctx, svcCtx)
		resp, err := l.GetSiteConfig()
		result.HttpResult(c, resp, err)
	}
}
