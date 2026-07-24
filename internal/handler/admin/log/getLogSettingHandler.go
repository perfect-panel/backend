package log

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// GetLogSettingHandler documents Get log setting.
//
// @Summary Get log setting
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} result.ResponseSuccessBean{data=dto.LogSetting}
// @Router /v1/admin/log/setting [get]
func GetLogSettingHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		resp, err := svcCtx.Platform.GetLogSetting(ctx)
		result.HttpResult(c, resp, err)
	}
}
