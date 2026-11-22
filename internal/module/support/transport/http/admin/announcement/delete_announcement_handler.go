package announcement

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// DeleteAnnouncementHandler documents Delete announcement.
//
// @Summary Delete announcement
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.DeleteAnnouncementRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/announcement/ [delete]
func DeleteAnnouncementHandler(service support.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.DeleteAnnouncementRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		err := service.DeleteAnnouncement(ctx, &req)
		httpx.HttpResult(c, nil, err)
	}
}
