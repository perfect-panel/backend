package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.SubscribeConfig

// GetSubscribeConfigHandler documents Get subscribe config.
//
// @Summary Get subscribe config
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.SubscribeConfig}
// @Router /v1/admin/system/subscribe_config [get]
func GetSubscribeConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetSubscribeConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
