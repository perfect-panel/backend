package subscribe

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.ResetAllSubscribeTokenResponse

// ResetAllSubscribeTokenHandler documents Reset all subscribe tokens.
//
// @Summary Reset all subscribe tokens
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.ResetAllSubscribeTokenResponse}
// @Router /v1/admin/subscribe/reset_all_token [post]
func ResetAllSubscribeTokenHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {

		resp, err := service.ResetAllSubscribeToken(c)
		httpx.HttpResult(ctx, resp, err)
	}
}
