package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Get OAuth Methods
func GetOAuthMethodsHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		l := user.NewGetOAuthMethodsLogic(c, svcCtx)
		resp, err := l.GetOAuthMethods()
		result.HttpResult(ctx, resp, err)
	}
}
