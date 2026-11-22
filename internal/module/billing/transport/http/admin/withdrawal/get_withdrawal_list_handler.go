// Package withdrawal holds the HTTP handlers of the admin commission
// withdrawal review API; they adapt requests to the billing facade.
package withdrawal

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetWithdrawalListHandler documents the admin withdrawal queue.
//
// @Summary Get withdrawal list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetWithdrawalListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetWithdrawalListResponse}
// @Router /v1/admin/withdrawal/list [get]
func GetWithdrawalListHandler(service billing.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetWithdrawalListRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		if err := validation.Validate(&req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.GetWithdrawalList(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
