package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// ResetPasswordService is the part of the identity facade
// ResetPasswordHandler calls.
type ResetPasswordService interface {
	ResetPassword(ctx context.Context, req *dto.ResetPasswordRequest) (*dto.LoginResponse, error)
}

var _ ResetPasswordService = identity.Service(nil)

// ResetPasswordHandler documents Reset password.
//
// The identity service applies the configured Turnstile check.
//
// @Summary Reset password
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.ResetPasswordRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/reset [post]
func ResetPasswordHandler(service ResetPasswordService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.ResetPasswordRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}
		resp, err := service.ResetPassword(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
