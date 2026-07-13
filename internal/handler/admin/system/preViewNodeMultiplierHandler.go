package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// PreView Node Multiplier
func PreViewNodeMultiplierHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := system.NewPreViewNodeMultiplierLogic(ctx, svcCtx)
		resp, err := l.PreViewNodeMultiplier()
		result.HttpResult(c, resp, err)
	}
}
