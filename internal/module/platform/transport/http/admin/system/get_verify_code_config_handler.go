package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
)

var _ dto.VerifyCodeConfig

// GetVerifyCodeConfigHandler documents Get Verify Code Config.
//
// @Summary Get Verify Code Config
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.VerifyCodeConfig}
// @Router /v1/admin/system/verify_code_config [get]
func GetVerifyCodeConfigHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := service.GetVerifyCodeConfig(ctx)
		httpx.HttpResult(c, resp, err)
	}
}
