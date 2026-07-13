package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/tool"
)

func RegisterTelegramHandlers(router *server.Hertz, serverCtx *svc.ServiceContext) {
	router.POST("/v1/telegram/webhook", TelegramHandler(serverCtx))
}

func TelegramHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		// auth secret
		secret := ctx.Query("secret")
		if secret != tool.Md5Encode(svcCtx.Config.Telegram.BotToken, false) {
			logger.WithContext(c).Error("[TelegramHandler] Secret is wrong", logger.Field("request secret", secret), logger.Field("config secret", tool.Md5Encode(svcCtx.Config.Telegram.BotToken, false)), logger.Field("token", svcCtx.Config.Telegram.BotToken))
			ctx.Abort()
			result.HttpResult(ctx, nil, nil)
			return
		}
		var request tgbotapi.Update
		if err := ctx.BindJSON(&request); err != nil {
			logger.WithContext(c).Error("[TelegramHandler] Failed to bind request", logger.Field("error", err.Error()))
			ctx.Abort()
			result.HttpResult(ctx, nil, err)
		}
		l := telegram.NewTelegramLogic(c, svcCtx)
		l.TelegramLogic(&request)
	}
}
