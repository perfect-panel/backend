package tool

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// RestartSystemHandler documents Restart System.
//
// @Summary Restart System
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/tool/restart [post]
func RestartSystemHandler(service platform.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		err := service.RestartSystem(ctx)
		httpx.HttpResult(c, nil, err)
	}
}
