package ticket

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetTicketListHandler documents Get ticket list.
//
// @Summary Get ticket list
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request query dto.GetTicketListRequest false "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean{data=dto.GetTicketListResponse}
// @Router /v1/admin/ticket/list [get]
func GetTicketListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.GetTicketListRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		resp, err := svcCtx.Support.GetTicketList(ctx, &req)
		result.HttpResult(c, resp, err)
	}
}
