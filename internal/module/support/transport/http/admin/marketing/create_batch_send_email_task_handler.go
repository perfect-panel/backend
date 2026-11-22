// Package marketing holds the Hertz handlers of the admin marketing tasks:
// batch email campaigns and quota gifts.
package marketing

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// CreateBatchSendEmailTaskHandler documents Create a batch send email task.
//
// @Summary Create a batch send email task
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreateBatchSendEmailTaskRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/marketing/email/batch/send [post]
func CreateBatchSendEmailTaskHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.CreateBatchSendEmailTaskRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		err := service.CreateBatchSendEmailTask(c, &req)
		httpx.HttpResult(ctx, nil, err)
	}
}
