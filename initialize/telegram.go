package initialize

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Telegram(svc *svc.ServiceContext) {

	method, err := svc.Store.Auth().FindOneByMethod(context.Background(), "telegram")
	if err != nil {
		logger.Errorf("[Init Telegram Config] Get Telegram Config Error: %s", err.Error())
		return
	}
	tgConfig := new(auth.TelegramAuthConfig)
	if err = tgConfig.Unmarshal(method.Config); err != nil {
		logger.Errorf("[Init Telegram Config] Unmarshal Telegram Config Error: %s", err.Error())
		return
	}

	if tgConfig.BotToken == "" {
		logger.Debug("[Init Telegram Config] Telegram Token is empty")
		return
	}

	bot, err := tgbotapi.NewBotAPI(tgConfig.BotToken)
	if err != nil {
		logger.Error("[Init Telegram Config] New Bot API Error: ", logger.Field("error", err.Error()))
		return
	}

	// Pick mode: prefer webhook, fall back to long-polling when no domain or in debug.
	useWebhook := tgConfig.WebHookDomain != "" && !svc.Config.Debug
	if useWebhook {
		// Webhook mode: register URL with Telegram
		webhookURL := fmt.Sprintf("%s/v1/telegram/webhook?secret=%s",
			tgConfig.WebHookDomain, tool.Md5Encode(tgConfig.BotToken, false))
		wh, err := tgbotapi.NewWebhook(webhookURL)
		if err != nil {
			logger.Errorf("[Init Telegram Config] New Webhook Error: %s", err.Error())
			return
		}
		if _, err = bot.Request(wh); err != nil {
			logger.Errorf("[Init Telegram Config] Request Webhook Error: %s", err.Error())
			return
		}
		logger.Info("[Init Telegram Config] Webhook registered", logger.Field("url", webhookURL))
	} else {
		// Long Polling mode
		updateConfig := tgbotapi.NewUpdate(0)
		updateConfig.Timeout = 60
		updates := bot.GetUpdatesChan(updateConfig)
		go func() {
			for update := range updates {
				if update.Message != nil {
					ctx := context.Background()
					l := telegram.NewTelegramLogic(ctx, svc)
					l.TelegramLogic(&update)
				}
			}
		}()
		mode := "long-polling"
		if svc.Config.Debug {
			mode = "long-polling (debug)"
		}
		logger.Info("[Init Telegram Config] Using " + mode)
	}

	user, err := bot.GetMe()
	if err != nil {
		logger.Error("[Init Telegram Config] Get Bot Info Error: ", logger.Field("error", err.Error()))
		return
	}
	svc.Config.Telegram = config.Telegram{
		Enable:        method.Enabled != nil && *method.Enabled,
		BotID:         user.ID,
		BotName:       user.UserName,
		BotToken:      tgConfig.BotToken,
		EnableNotify:  tgConfig.EnableNotify,
		WebHookDomain: tgConfig.WebHookDomain,
	}
	svc.TelegramBot = bot

	logger.Info("[Init Telegram Config] Telegram init success")
}
