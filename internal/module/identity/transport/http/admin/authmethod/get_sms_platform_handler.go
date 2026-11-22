package authmethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.AuthPlatformResponse

// GetSmsPlatformHandler documents Get sms support platform.
//
// @Summary Get sms support platform
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.AuthPlatformResponse}
// @Router /v1/admin/auth-method/sms_platform [get]
func GetSmsPlatformHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetSmsPlatform(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
