package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetUserSubscribeDevicesHandler documents Get user subcribe devices.
//
// @Summary Get user subcribe devices
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetUserSubscribeDevicesRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetUserSubscribeDevicesResponse}
// @Router /v1/admin/user/subscribe/device [get]
func GetUserSubscribeDevicesHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetUserSubscribeDevicesRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetUserSubscribeDevices(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
