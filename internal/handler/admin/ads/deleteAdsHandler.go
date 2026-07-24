package ads

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// DeleteAdsHandler documents Delete Ads.
//
// @Summary Delete Ads
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DeleteAdsRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/admin/ads/ [delete]
func DeleteAdsHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.DeleteAdsRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		err := svcCtx.Support.DeleteAds(ctx, &req)
		result.HttpResult(c, nil, err)
	}
}
