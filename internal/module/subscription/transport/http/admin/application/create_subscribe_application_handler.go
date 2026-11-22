// Package application holds the admin HTTP handlers for the client
// applications of subscription delivery and their template previews.
package application

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CreateSubscribeApplicationHandler documents Create subscribe application.
//
// @Summary Create subscribe application
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreateSubscribeApplicationRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.SubscribeApplication}
// @Router /v1/admin/application/ [post]
func CreateSubscribeApplicationHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.CreateSubscribeApplicationRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.CreateSubscribeApplication(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
