package document

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetDocumentDetailHandler documents Get document detail.
//
// @Summary Get document detail
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetDocumentDetailRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.Document}
// @Router /v1/admin/document/detail [get]
func GetDocumentDetailHandler(service support.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetDocumentDetailRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetDocumentDetail(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
