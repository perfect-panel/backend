// Package tool holds the Hertz handlers of the admin tools: the system log
// tail, the version, IP geolocation and the server restart.
package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.LogResponse

// GetSystemLogHandler documents Get System Log.
//
// @Summary Get System Log
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.LogResponse}
// @Router /v1/admin/tool/log [get]
func GetSystemLogHandler(service platform.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetSystemLog(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
