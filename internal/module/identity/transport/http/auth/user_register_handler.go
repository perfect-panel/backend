package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UserRegisterService is the part of the identity facade UserRegisterHandler
// calls.
type UserRegisterService interface {
	UserRegister(ctx context.Context, req *dto.UserRegisterRequest) (*dto.LoginResponse, error)
}

var _ UserRegisterService = identity.Service(nil)

// UserRegisterHandler documents registers a user..
//
// @Summary registers a user.
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.UserRegisterRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/register [post]
func UserRegisterHandler(service UserRegisterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UserRegisterRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.UserRegister(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
