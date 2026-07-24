package portal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// PurchaseHandler documents Purchase subscription.
//
// @Summary Purchase subscription
// @Tags user
// @Accept json
// @Produce json
// @Param request body dto.PortalPurchaseRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.PortalPurchaseResponse}
// @Router /v1/public/portal/purchase [post]
func PurchaseHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.PortalPurchaseRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := svcCtx.Billing.PortalPurchase(c, &req)
		result.HttpResult(ctx, resp, err)
	}
}
