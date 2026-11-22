package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// TelephoneUserRegisterService is the part of the identity facade
// TelephoneUserRegisterHandler calls.
type TelephoneUserRegisterService interface {
	TelephoneUserRegister(ctx context.Context, req *dto.TelephoneRegisterRequest) (*dto.LoginResponse, error)
}

var _ TelephoneUserRegisterService = identity.Service(nil)

// TelephoneUserRegisterHandler documents User Telephone register.
//
// @Summary User Telephone register
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.TelephoneRegisterRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/register/telephone [post]
func TelephoneUserRegisterHandler(service TelephoneUserRegisterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.TelephoneRegisterRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}
		resp, err := service.TelephoneUserRegister(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
