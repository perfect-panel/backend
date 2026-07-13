package authMethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/authMethod"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get auth method list
func GetAuthMethodListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		l := authMethod.NewGetAuthMethodListLogic(ctx, svcCtx)
		resp, err := l.GetAuthMethodList()
		result.HttpResult(c, resp, err)
	}
}
