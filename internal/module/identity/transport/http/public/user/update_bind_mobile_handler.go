package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateBindMobileHandler documents Update Bind Mobile.
//
// @Summary Update Bind Mobile
// @Description Binds the phone number proven by the code sent to it. Replacing the one the account already has also requires the current password (password), or, for an account without a password, the security code sent to the current phone number (current_code), and ends every session of the account.
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateBindMobileRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/public/user/bind_mobile [put]
func UpdateBindMobileHandler(service identity.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.UpdateBindMobileRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.UpdateBindMobile(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
