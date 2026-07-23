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

// OAuthLoginGetTokenHandler documents OAuth login get token.
//
// @Summary OAuth login get token
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.OAuthLoginGetTokenRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/oauth/login/token [post]
func OAuthLoginGetTokenHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.OAuthLoginGetTokenRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := oauth.NewOAuthLoginGetTokenLogic(ctx, oauth.OAuthLoginDependencies{
			Store: svcCtx.Store,
			Redis: svcCtx.Redis,
			Config: oauth.OAuthLoginConfig{
				InviteForced:            svcCtx.Config.Invite.ForcedInvite,
				OnlyFirstPurchase:       svcCtx.Config.Invite.OnlyFirstPurchase,
				EmailDomainSuffixList:   svcCtx.Config.Email.DomainSuffixList,
				EmailEnableDomainSuffix: svcCtx.Config.Email.EnableDomainSuffix,
				TrialEnabled:            svcCtx.Config.Register.EnableTrial,
				TrialSubscribeID:        svcCtx.Config.Register.TrialSubscribe,
				TrialTime:               svcCtx.Config.Register.TrialTime,
				TrialTimeUnit:           svcCtx.Config.Register.TrialTimeUnit,
				JWTAccessSecret:         svcCtx.Config.JwtAuth.AccessSecret,
				JWTAccessExpire:         svcCtx.Config.JwtAuth.AccessExpire,
			},
			Policy: registerpolicy.NewServicePolicy(svcCtx),
		})
		resp, err := l.OAuthLoginGetToken(&req, c.ClientIP(), string(c.UserAgent()))
		result.HttpResult(c, resp, err)
	}
}
