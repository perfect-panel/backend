package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// KickOfflineByUserDeviceHandler documents kick offline user device.
//
// @Summary kick offline user device
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.KickOfflineRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/user/device/kick_offline [put]
func KickOfflineByUserDeviceHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.KickOfflineRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.KickOfflineByUserDevice(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
