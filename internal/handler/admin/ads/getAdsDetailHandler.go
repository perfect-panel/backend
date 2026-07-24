package ads

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetAdsDetailHandler documents Get Ads Detail.
//
// @Summary Get Ads Detail
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetAdsDetailRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.Ads}
// @Router /v1/admin/ads/detail [get]
func GetAdsDetailHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetAdsDetailRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Support.GetAdsDetail(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
