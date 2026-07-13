package authMethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/authMethod"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get email support platform
func GetEmailPlatformHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := authMethod.NewGetEmailPlatformLogic(ctx, svcCtx)
		resp, err := l.GetEmailPlatform()
		result.HttpResult(c, resp, err)
	}
}
