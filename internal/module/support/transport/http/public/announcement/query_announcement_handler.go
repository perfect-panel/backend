// Package announcement holds the Hertz handler of the announcements users
// see.
package announcement

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// QueryAnnouncementHandler documents Query announcement.
//
// @Summary Query announcement
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.QueryAnnouncementRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.QueryAnnouncementResponse}
// @Router /v1/public/announcement/list [get]
func QueryAnnouncementHandler(service support.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.QueryAnnouncementRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(ctx, validateErr)
			return
		}

		resp, err := service.QueryAnnouncement(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
