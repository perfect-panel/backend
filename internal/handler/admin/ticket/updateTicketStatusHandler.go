package ticket

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/logic/admin/ticket"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/result"
)

// Update ticket status
func UpdateTicketStatusHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req types.UpdateTicketStatusRequest
		_ = httpx.ShouldBind(c, &req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := ticket.NewUpdateTicketStatusLogic(ctx, svcCtx)
		err := l.UpdateTicketStatus(&req)
		result.HttpResult(c, nil, err)
	}
}
