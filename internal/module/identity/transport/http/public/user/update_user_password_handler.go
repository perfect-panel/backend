package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateUserPasswordService is the part of the identity facade
// UpdateUserPasswordHandler calls.
type UpdateUserPasswordService interface {
	UpdateUserPassword(ctx context.Context, req *dto.UpdateUserPasswordRequest) (*dto.UpdateUserPasswordResponse, error)
}

var _ UpdateUserPasswordService = identity.Service(nil)

// UpdateUserPasswordHandler documents Update User Password.
//
// @Summary Update User Password
// @Description Sets the account's password and ends every session of the account. Changing an existing password requires the current one (old_password); setting the first password of an account with a bound email or phone number requires the security code sent to it (current_code); an account with neither sets it with the session alone. The response lists the third-party sign-in methods (OAuth providers, Telegram) still bound to the account, which the change does not remove.
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateUserPasswordRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.UpdateUserPasswordResponse}
// @Router /v1/public/user/password [put]
func UpdateUserPasswordHandler(service UpdateUserPasswordService) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.UpdateUserPasswordRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.UpdateUserPassword(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
