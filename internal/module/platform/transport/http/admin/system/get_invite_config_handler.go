package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.InviteConfig

// GetInviteConfigHandler documents Get invite config.
//
// @Summary Get invite config
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.InviteConfig}
// @Router /v1/admin/system/invite_config [get]
func GetInviteConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetInviteConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
