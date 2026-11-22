package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetPreSendEmailCountHandler documents Get pre-send email count.
//
// @Summary Get pre-send email count
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.GetPreSendEmailCountRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetPreSendEmailCountResponse}
// @Router /v1/admin/marketing/email/batch/pre-send-count [post]
func GetPreSendEmailCountHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.GetPreSendEmailCountRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.GetPreSendEmailCount(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
