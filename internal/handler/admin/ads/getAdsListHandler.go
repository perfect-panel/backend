package ads

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetAdsListHandler documents Get Ads List.
//
// @Summary Get Ads List
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetAdsListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetAdsListResponse}
// @Router /v1/admin/ads/list [get]
func GetAdsListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetAdsListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Support.GetAdsList(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
