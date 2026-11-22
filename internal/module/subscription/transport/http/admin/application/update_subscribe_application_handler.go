package application

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// UpdateSubscribeApplicationHandler documents Update subscribe application.
//
// @Summary Update subscribe application
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateSubscribeApplicationRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.SubscribeApplication}
// @Router /v1/admin/application/subscribe_application [put]
func UpdateSubscribeApplicationHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UpdateSubscribeApplicationRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.UpdateSubscribeApplication(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
