package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetGlobalConfigResponse

// GetGlobalConfigHandler documents Get global config.
//
// @Summary Get global config
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetGlobalConfigResponse}
// @Router /v1/common/site/config [get]
func GetGlobalConfigHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		resp, err := service.GetGlobalConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
