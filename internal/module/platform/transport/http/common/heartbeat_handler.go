package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.HeartbeatResponse

// HeartbeatHandler documents Heartbeat.
//
// @Summary Heartbeat
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.HeartbeatResponse}
// @Router /v1/common/heartbeat [get]
func HeartbeatHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.Heartbeat(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
