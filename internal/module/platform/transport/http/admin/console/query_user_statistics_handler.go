package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.UserStatisticsResponse

// QueryUserStatisticsHandler documents Query user statistics.
//
// @Summary Query user statistics
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.UserStatisticsResponse}
// @Router /v1/admin/console/user [get]
func QueryUserStatisticsHandler(service Dashboard) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.QueryUserStatistics(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
