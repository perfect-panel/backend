package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetNodeMultiplierResponse

// GetNodeMultiplierHandler documents Get Node Multiplier.
//
// @Summary Get Node Multiplier
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetNodeMultiplierResponse}
// @Router /v1/admin/system/get_node_multiplier [get]
func GetNodeMultiplierHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetNodeMultiplier(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
