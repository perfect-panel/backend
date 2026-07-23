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

// TelegramHandler documents Telegram.
//
// @Summary Telegram
// @Tags common
// @Accept json
// @Produce json
// @Security TelegramSecret
// @Param request body object true "Telegram Bot API update"
// @Success 200 {object} result.ResponseSuccessBean
// @Router /v1/telegram/webhook [post]
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
		l := newTelegramLogic(c, svcCtx)
		l.TelegramLogic(&request)
	}
}

func newTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *telegram.TelegramLogic {
	messenger := telegram.NewTelegramBotMessenger(svcCtx.TelegramBot)
	redisStore := telegram.NewTelegramRedisStore(svcCtx.Redis)
	admin := telegram.NewTelegramAdmin(ctx, telegram.TelegramAdminDependencies{
		Messenger:     messenger,
		Actions:       redisStore,
		Tickets:       svcCtx.Store.Ticket(),
		Orders:        svcCtx.Store.Order(),
		Users:         svcCtx.Store.User(),
		UserAuth:      svcCtx.Store.UserAuth(),
		Subscriptions: svcCtx.Store.UserSubscription(),
		UserCache:     svcCtx.Store.UserCache(),
		Plans:         svcCtx.Store.Subscribe(),
		Logs:          svcCtx.Store.Log(),
	})
	return telegram.NewTelegramLogic(ctx, telegram.TelegramLogicDependencies{
		Messenger: messenger,
		Sessions:  redisStore,
		UserAuth:  svcCtx.Store.UserAuth(),
		UserCache: svcCtx.Store.UserCache(),
		Admin:     admin,
	})
}
