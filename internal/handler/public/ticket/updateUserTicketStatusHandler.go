package ticket

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// UpdateUserTicketStatusHandler documents Update ticket status.
//
// @Summary Update ticket status
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.UpdateUserTicketStatusRequest true "Request parameters"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/public/ticket/ [put]
func UpdateUserTicketStatusHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.UpdateUserTicketStatusRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			result.ParamErrorResult(ctx, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(ctx, validateErr)
			return
		}

		err := svcCtx.Support.UpdateUserTicketStatus(c, &req)
		result.HttpResult(ctx, nil, err)
	}
}
