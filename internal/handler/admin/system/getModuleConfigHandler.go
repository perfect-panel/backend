package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// GetModuleConfigHandler Get Module Config
func GetModuleConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := system.NewGetModuleConfigLogic(ctx, svcCtx)
		resp, err := l.GetModuleConfig()
		result.HttpResult(c, resp, err)
	}
}
