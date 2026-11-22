package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.GetStatResponse

// GetStatHandler documents Get stat.
//
// @Summary Get stat
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetStatResponse}
// @Router /v1/common/site/stat [get]
func GetStatHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetStat(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
