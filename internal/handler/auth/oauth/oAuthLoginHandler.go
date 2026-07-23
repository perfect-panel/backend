package oauth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/auth/oauth"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// OAuthLoginHandler documents OAuth login.
//
// @Summary OAuth login
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.OAthLoginRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.OAuthLoginResponse}
// @Router /v1/auth/oauth/login [post]
func OAuthLoginHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.OAthLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := oauth.NewOAuthLoginLogic(ctx, oauth.OAuthLoginURLDependencies{
			Store:  svcCtx.Store,
			Redis:  svcCtx.Redis,
			Policy: registerpolicy.NewServicePolicy(svcCtx),
		})
		resp, err := l.OAuthLogin(&req)
		result.HttpResult(c, resp, err)
	}
}
