package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.PrivacyPolicyConfig

// GetPrivacyPolicyConfigHandler documents get Privacy Policy Config.
//
// @Summary get Privacy Policy Config
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PrivacyPolicyConfig}
// @Router /v1/admin/system/privacy [get]
func GetPrivacyPolicyConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetPrivacyPolicyConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
