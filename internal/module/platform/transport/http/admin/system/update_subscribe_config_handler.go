package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateSubscribeConfigHandler documents Update subscribe config.
//
// @Summary Update subscribe config
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.SubscribeConfig true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/system/subscribe_config [put]
func UpdateSubscribeConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.SubscribeConfig
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.UpdateSubscribeConfig(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
