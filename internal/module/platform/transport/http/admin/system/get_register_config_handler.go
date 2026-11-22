package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.RegisterConfig

// GetRegisterConfigHandler documents Get register config.
//
// @Summary Get register config
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.RegisterConfig}
// @Router /v1/admin/system/register_config [get]
func GetRegisterConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetRegisterConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
