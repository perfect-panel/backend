package oauth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// OAuthLoginGetTokenService is the part of the identity facade
// OAuthLoginGetTokenHandler calls.
type OAuthLoginGetTokenService interface {
	OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (*dto.LoginResponse, error)
}

var _ OAuthLoginGetTokenService = identity.Service(nil)

// OAuthLoginGetTokenHandler documents OAuth login get token.
//
// @Summary OAuth login get token
// @Description Completes a sign-in with the provider's callback and answers with the session token. A sign-in started with a nonce (/v1/auth/oauth/login) is completed only with the same nonce; one started without is completed only without.
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.OAuthLoginGetTokenRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/oauth/login/token [post]
func OAuthLoginGetTokenHandler(service OAuthLoginGetTokenService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.OAuthLoginGetTokenRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.OAuthLoginGetToken(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
