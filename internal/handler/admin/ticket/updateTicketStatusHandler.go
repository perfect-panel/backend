package ticket

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// UpdateTicketStatusHandler documents Update ticket status.
//
// @Summary Update ticket status
// @Tags admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateTicketStatusRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/admin/ticket/ [put]
func UpdateTicketStatusHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req dto.UpdateTicketStatusRequest
		if err := httpx.ShouldBind(c, &req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		err := svcCtx.Support.UpdateTicketStatus(ctx, &req)
		result.HttpResult(c, nil, err)
	}
}
