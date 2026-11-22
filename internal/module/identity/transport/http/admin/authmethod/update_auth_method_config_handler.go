package authmethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateAuthMethodConfigHandler documents Update auth method config.
//
// @Summary Update auth method config
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateAuthMethodConfigRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.AuthMethodConfig}
// @Router /v1/admin/auth-method/config [put]
func UpdateAuthMethodConfigHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UpdateAuthMethodConfigRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.UpdateAuthMethodConfig(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
