// Package subscribe holds the admin HTTP handlers for plans and plan groups.
package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// BatchDeleteSubscribeGroupHandler documents Batch delete subscribe group.
//
// @Summary Batch delete subscribe group
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.BatchDeleteSubscribeGroupRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/subscribe/group/batch [delete]
func BatchDeleteSubscribeGroupHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.BatchDeleteSubscribeGroupRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.BatchDeleteSubscribeGroup(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
