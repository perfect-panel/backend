package order

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryOrderListHandler documents Get order list.
//
// @Summary Get order list
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryOrderListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryOrderListResponse}
// @Router /v1/public/order/list [get]
func QueryOrderListHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryOrderListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryOrderList(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
