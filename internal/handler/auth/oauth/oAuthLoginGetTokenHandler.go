package oauth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth/oauth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// OAuth login get token
func OAuthLoginGetTokenHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req types.OAuthLoginGetTokenRequest
		_ = httpx.ShouldBind(c, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := oauth.NewOAuthLoginGetTokenLogic(ctx, svcCtx)
		resp, err := l.OAuthLoginGetToken(&req, c.ClientIP(), string(c.UserAgent()))
		result.HttpResult(c, resp, err)
	}
}
