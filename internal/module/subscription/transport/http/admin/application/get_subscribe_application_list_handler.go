package application

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetSubscribeApplicationListHandler documents Get subscribe application list.
//
// @Summary Get subscribe application list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetSubscribeApplicationListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetSubscribeApplicationListResponse}
// @Router /v1/admin/application/subscribe_application_list [get]
func GetSubscribeApplicationListHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetSubscribeApplicationListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetSubscribeApplicationList(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
