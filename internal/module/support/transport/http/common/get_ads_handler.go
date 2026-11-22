// Package common holds the Hertz handler of the ads the public site shows.
package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetAdsHandler documents Get Ads.
//
// @Summary Get Ads
// @Tags common
// @Accept json
// @Produce json
// @Param request query dto.GetAdsRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetAdsResponse}
// @Router /v1/common/ads [get]
func GetAdsHandler(service support.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetAdsRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetPublicAds(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
