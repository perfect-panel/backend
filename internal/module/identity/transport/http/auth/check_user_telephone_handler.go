package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CheckUserTelephoneHandler documents Check user telephone is exist.
//
// @Summary Check user telephone is exist
// @Tags common
// @Accept json
// @Produce json
// @Param request query dto.TelephoneCheckUserRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.TelephoneCheckUserResponse}
// @Router /v1/auth/check/telephone [get]
func CheckUserTelephoneHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.TelephoneCheckUserRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.CheckUserTelephone(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
