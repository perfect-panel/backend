package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// QueryUserAffiliateHandler documents Query User Affiliate Count.
//
// @Summary Query User Affiliate Count
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} result.ResponseSuccessBean{data=dto.QueryUserAffiliateCountResponse}
// @Router /v1/public/user/affiliate/count [get]
func QueryUserAffiliateHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := svcCtx.Billing.QueryUserAffiliate(c)
		result.HttpResult(ctx, resp, err)
	}
}
