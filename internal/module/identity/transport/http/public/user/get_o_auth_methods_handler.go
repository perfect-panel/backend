package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetOAuthMethodsResponse

// GetOAuthMethodsService is the part of the identity facade
// GetOAuthMethodsHandler calls.
type GetOAuthMethodsService interface {
	GetOAuthMethods(ctx context.Context) (*dto.GetOAuthMethodsResponse, error)
}

var _ GetOAuthMethodsService = identity.Service(nil)

// GetOAuthMethodsHandler documents Get OAuth Methods.
//
// @Summary Get OAuth Methods
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetOAuthMethodsResponse}
// @Router /v1/public/user/oauth_methods [get]
func GetOAuthMethodsHandler(service GetOAuthMethodsService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.GetOAuthMethods(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
