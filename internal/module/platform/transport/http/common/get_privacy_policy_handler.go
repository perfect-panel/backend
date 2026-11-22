package common

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.PrivacyPolicyConfig

// GetPrivacyPolicyHandler documents Get Privacy Policy.
//
// @Summary Get Privacy Policy
// @Tags common
// @Produce json
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.PrivacyPolicyConfig}
// @Router /v1/common/site/privacy [get]
func GetPrivacyPolicyHandler(service PublicInfo) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetPrivacyPolicy(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
