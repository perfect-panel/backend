package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.QueryUserAffiliateCountResponse

// QueryUserAffiliateHandler documents Query User Affiliate Count.
//
// @Summary Query User Affiliate Count
// @Tags user
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryUserAffiliateCountResponse}
// @Router /v1/public/user/affiliate/count [get]
func QueryUserAffiliateHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.QueryUserAffiliate(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
