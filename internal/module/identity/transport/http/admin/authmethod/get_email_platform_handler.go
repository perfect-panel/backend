package authmethod

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/identity"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.AuthPlatformResponse

// GetEmailPlatformHandler documents Get email support platform.
//
// @Summary Get email support platform
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.AuthPlatformResponse}
// @Router /v1/admin/auth-method/email_platform [get]
func GetEmailPlatformHandler(service identity.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetEmailPlatform(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
