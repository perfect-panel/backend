// Package document holds the Hertz handlers of the help documents users read.
package document

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryDocumentDetailHandler documents Get document detail.
//
// @Summary Get document detail
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryDocumentDetailRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.Document}
// @Router /v1/public/document/detail [get]
func QueryDocumentDetailHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryDocumentDetailRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryDocumentDetail(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
