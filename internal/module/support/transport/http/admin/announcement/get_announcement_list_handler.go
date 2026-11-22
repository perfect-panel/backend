package announcement

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetAnnouncementListHandler documents Get announcement list.
//
// @Summary Get announcement list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetAnnouncementListRequest false "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.GetAnnouncementListResponse}
// @Router /v1/admin/announcement/list [get]
func GetAnnouncementListHandler(service support.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetAnnouncementListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			httpx.ParamErrorResult(c, err)
			return
		}
		validateErr := validation.Validate(&req)
		if validateErr != nil {
			httpx.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := service.GetAnnouncementList(ctx, &req)
		httpx.HttpResult(c, resp, err)
	}
}
