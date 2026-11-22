package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateTosConfigHandler documents Update Team of Service Config.
//
// @Summary Update Team of Service Config
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.TosConfig true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/system/tos_config [put]
func UpdateTosConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.TosConfig
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.UpdateTosConfig(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
