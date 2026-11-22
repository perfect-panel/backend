package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.PreViewNodeMultiplierResponse

// PreViewNodeMultiplierHandler documents PreView Node Multiplier.
//
// @Summary PreView Node Multiplier
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PreViewNodeMultiplierResponse}
// @Router /v1/admin/system/node_multiplier/preview [get]
func PreViewNodeMultiplierHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.PreViewNodeMultiplier(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
