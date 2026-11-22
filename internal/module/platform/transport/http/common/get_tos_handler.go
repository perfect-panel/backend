package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetTosResponse

// GetTosHandler documents Get Tos Content.
//
// @Summary Get Tos Content
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetTosResponse}
// @Router /v1/common/site/tos [get]
func GetTosHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetTos(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
