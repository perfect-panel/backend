package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get Verify Code Config
func GetVerifyCodeConfigHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := system.NewGetVerifyCodeConfigLogic(ctx, svcCtx)
		resp, err := l.GetVerifyCodeConfig()
		result.HttpResult(c, resp, err)
	}
}
