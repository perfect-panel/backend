package console

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.TicketWaitRelpyResponse

// QueryTicketWaitReplyHandler documents Query ticket wait reply.
//
// @Summary Query ticket wait reply
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.TicketWaitRelpyResponse}
// @Router /v1/admin/console/ticket [get]
func QueryTicketWaitReplyHandler(service Dashboard) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.QueryTicketWaitReply(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
