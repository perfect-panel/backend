// Package user holds the HTTP handlers through which users manage their own
// subscriptions.
package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetSubscribeLogHandler documents Get Subscribe Log.
//
// @Summary Get Subscribe Log
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetSubscribeLogRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetSubscribeLogResponse}
// @Router /v1/public/user/subscribe_log [get]
func GetSubscribeLogHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetSubscribeLogRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetSubscribeLog(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
