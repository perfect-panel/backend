package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// TelephoneLoginService is the part of the identity facade
// TelephoneLoginHandler calls.
type TelephoneLoginService interface {
	TelephoneLogin(ctx context.Context, req *dto.TelephoneLoginRequest) (*dto.LoginResponse, error)
}

var _ TelephoneLoginService = identity.Service(nil)

// TelephoneLoginHandler documents User Telephone login.
//
// The identity service applies the configured Turnstile check.
//
// @Summary User Telephone login
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.TelephoneLoginRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/login/telephone [post]
func TelephoneLoginHandler(service TelephoneLoginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.TelephoneLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}
		resp, err := service.TelephoneLogin(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
