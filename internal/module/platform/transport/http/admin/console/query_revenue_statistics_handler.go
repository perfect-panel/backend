// Package console holds the Hertz handlers of the admin console dashboard:
// the revenue, user, traffic and ticket figures the platform facade computes.
package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.RevenueStatisticsResponse

// QueryRevenueStatisticsHandler documents Query revenue statistics.
//
// @Summary Query revenue statistics
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.RevenueStatisticsResponse}
// @Router /v1/admin/console/revenue [get]
func QueryRevenueStatisticsHandler(service Dashboard) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.QueryRevenueStatistics(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
