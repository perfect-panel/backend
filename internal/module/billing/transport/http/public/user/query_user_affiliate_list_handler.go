package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryUserAffiliateListHandler documents Query User Affiliate List.
//
// @Summary Query User Affiliate List
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryUserAffiliateListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryUserAffiliateListResponse}
// @Router /v1/public/user/affiliate/list [get]
func QueryUserAffiliateListHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryUserAffiliateListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryUserAffiliateList(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
