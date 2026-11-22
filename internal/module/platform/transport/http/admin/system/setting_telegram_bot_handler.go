package system

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// SettingTelegramBotHandler documents setting telegram bot.
//
// @Summary setting telegram bot
// @Tags admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/admin/system/setting_telegram_bot [post]
func SettingTelegramBotHandler(service Settings) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {

		err := service.SettingTelegramBot(ctx)
		httpx.HttpResult(c, nil, err)
	}
}
