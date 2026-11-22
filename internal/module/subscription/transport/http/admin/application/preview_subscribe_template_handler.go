package application

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// PreviewSubscribeTemplateHandler documents Preview Template.
//
// @Summary Preview Template
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.PreviewSubscribeTemplateRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PreviewSubscribeTemplateResponse}
// @Router /v1/admin/application/preview [get]
func PreviewSubscribeTemplateHandler(service subscription.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.PreviewSubscribeTemplateRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.PreviewSubscribeTemplate(ctx, &req)
		httpx.HttpResult(c, resp, err)

	}
}
