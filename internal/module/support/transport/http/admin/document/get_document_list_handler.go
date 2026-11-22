package document

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetDocumentListHandler documents Get document list.
//
// @Summary Get document list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetDocumentListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetDocumentListResponse}
// @Router /v1/admin/document/list [get]
func GetDocumentListHandler(service support.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetDocumentListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetDocumentList(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
