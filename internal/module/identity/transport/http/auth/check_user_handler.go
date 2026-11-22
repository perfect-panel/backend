package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CheckUserHandler documents Check user is exist.
//
// @Summary Check user is exist
// @Tags common
// @Accept json
// @Produce json
// @Param request query dto.CheckUserRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.CheckUserResponse}
// @Router /v1/auth/check [get]
func CheckUserHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.CheckUserRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.CheckUser(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
