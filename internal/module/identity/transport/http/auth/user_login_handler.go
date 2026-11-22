// Package auth holds the identity module's handlers of the sign-in,
// registration and password-reset endpoints under /v1/auth.
package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UserLoginService is the part of the identity facade UserLoginHandler
// calls.
type UserLoginService interface {
	UserLogin(ctx context.Context, req *dto.UserLoginRequest) (*dto.LoginResponse, error)
}

var _ UserLoginService = identity.Service(nil)

// UserLoginHandler documents User login.
//
// The identity service applies the configured Turnstile check.
//
// @Summary User login
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.UserLoginRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/login [post]
func UserLoginHandler(service UserLoginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UserLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.UserLogin(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
