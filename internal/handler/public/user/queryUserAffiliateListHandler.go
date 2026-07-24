package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserAffiliateListHandler documents Query User Affiliate List.
//
// @Summary Query User Affiliate List
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryUserAffiliateListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserAffiliateListResponse}
// @Router /v1/public/user/affiliate/list [get]
func QueryUserAffiliateListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryUserAffiliateListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.QueryUserAffiliateList(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
