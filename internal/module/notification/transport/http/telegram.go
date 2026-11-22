// Package handler holds the Hertz handler of the Telegram bot webhook.
package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
)

// RegisterTelegramHandlers registers the Telegram bot webhook; botToken
// reads the current bot token, which the webhook secret derives from.
func RegisterTelegramHandlers(router *server.Hertz, service notification.Service, botToken func() string) {
	router.POST("/v1/telegram/webhook", TelegramHandler(service, botToken))
}

// WebhookUpdates is the part of the notification facade the webhook feeds:
// it decodes and handles one update.
type WebhookUpdates interface {
	HandleTelegramWebhook(ctx context.Context, payload []byte) error
}

// TelegramHandler documents Telegram.
//
// @Summary Telegram
// @Tags common
// @Accept json
// @Produce json
// @Security TelegramSecret
// @Param request body object true "Telegram Bot API update"
// @Success 200 {object} httpx.ResponseSuccessBean
// @Router /v1/telegram/webhook [post]
func TelegramHandler(service WebhookUpdates, botToken func() string) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		// Telegram echoes back the secret registered with setWebhook. The
		// comparison is constant-time and the log line reveals neither the
		// expected secret nor the bot token.
		secret := string(ctx.GetHeader("X-Telegram-Bot-Api-Secret-Token"))
		token := botToken()
		if token == "" || !notification.WebhookSecretEqual(secret, notification.WebhookSecret(token)) {
			logger.WithContext(c).Error("[TelegramHandler] webhook secret mismatch")
			ctx.Abort()
			httpx.HttpResult(ctx, nil, nil)
			return
		}
		// A payload Telegram signed correctly but this side cannot process is
		// logged and acknowledged: returning an error would only make
		// Telegram redeliver the same payload.
		if err := service.HandleTelegramWebhook(c, ctx.Request.Body()); err != nil {
			logger.WithContext(c).Error("[TelegramHandler] handle update failed", logger.Field("error", err.Error()))
		}
		httpx.HttpResult(ctx, nil, nil)
	}
}
