package oauth

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth/oauth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Apple Login Callback
func AppleLoginCallbackHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req types.AppleLoginCallbackRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request data"})
			return
		}
		l := oauth.NewAppleLoginCallbackLogic(c, svcCtx)
		redirect, err := l.AppleLoginCallback(&req)
		if err != nil {
			result.HttpResult(ctx, nil, err)
			return
		}
		ctx.Redirect(redirect.StatusCode, []byte(redirect.Location))
	}
}
