package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// DeviceLoginService is the part of the identity facade DeviceLoginHandler
// calls.
type DeviceLoginService interface {
	DeviceLogin(ctx context.Context, req *dto.DeviceLoginRequest) (*dto.LoginResponse, error)
}

var _ DeviceLoginService = identity.Service(nil)

// DeviceLoginHandler documents Device Login.
//
// @Summary Device Login
// @Description When device security is enabled, requests and response data use a signed data/time/sign envelope. Each request needs a fresh nonce; see docs/design/device-authentication.md. The User-Agent is read from the HTTP header.
// @Tags common
// @Accept json
// @Produce json
// @Param request body dto.DeviceLoginRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LoginResponse}
// @Router /v1/auth/login/device [post]
func DeviceLoginHandler(service DeviceLoginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.DeviceLoginRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}
		resp, err := service.DeviceLogin(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
