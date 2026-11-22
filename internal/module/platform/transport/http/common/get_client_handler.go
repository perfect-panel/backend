// Package common holds the Hertz handlers of the unauthenticated site-level
// reads: the global configuration, the terms of service and privacy policy,
// the site statistics, the client downloads and the heartbeat.
package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetSubscribeClientResponse

// GetClientHandler documents Get Client.
//
// @Summary Get Client
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetSubscribeClientResponse}
// @Router /v1/common/client [get]
func GetClientHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		resp, err := service.GetClient(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
