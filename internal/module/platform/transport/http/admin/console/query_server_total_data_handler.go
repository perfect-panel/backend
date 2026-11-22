package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.ServerTotalDataResponse

// QueryServerTotalDataHandler documents Query server total data.
//
// @Summary Query server total data
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.ServerTotalDataResponse}
// @Router /v1/admin/console/server [get]
func QueryServerTotalDataHandler(service Dashboard) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.QueryServerTotalData(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
