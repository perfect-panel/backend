package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/pkg/logger"
)

// telegramPoll tracks the long-polling loop across re-initialisations. The
// previous poller must stop before a new bot starts, otherwise two getUpdates
// consumers race on the same token and Telegram rejects one with 409.
var telegramPoll struct {
	sync.Mutex
	cancel context.CancelFunc
}

// swapTelegramPoller cancels the previous polling loop and, when start is
// non-nil, installs the next one. Cancel and install share one critical
// section so two concurrent re-initialisations cannot leak a poller.
func swapTelegramPoller(start func(ctx context.Context)) {
	telegramPoll.Lock()
	defer telegramPoll.Unlock()
	if telegramPoll.cancel != nil {
		telegramPoll.cancel()
		telegramPoll.cancel = nil
	}
	if start != nil {
		// The poller serves every later update, not the startup or reload
		// that starts it: it runs on a root context, cancelled only by the
		// next swap, rather than on one carrying that caller's trace.
		ctx, cancel := context.WithCancel(context.Background())
		telegramPoll.cancel = cancel
		go start(ctx)
	}
}

// telegramUpdateHandler handles the updates the long-polling loop receives.
// The bot library calls handlers without recover, so a panic in any update
// handler would take down the whole API process; it is logged with its
// stack and the update dropped instead. Webhook updates arrive through the
// HTTP server, whose default recovery middleware already contains panics.
func telegramUpdateHandler(deps *Dependencies) tgbot.HandlerFunc {
	return func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				logger.WithContext(ctx).Error("[Telegram Bot] update handler panicked",
					logger.Field("panic", fmt.Sprint(r)),
					logger.Field("update_id", update.ID),
					logger.Field("stack", string(debug.Stack())))
			}
		}()
		// Detach from the poller's lifetime: cancelling the poller on
		// re-initialisation must not abort an update mid-handling.
		deps.Notification.HandleTelegramUpdate(context.WithoutCancel(ctx), update)
	}
}

// Telegram (re)initialises the bot from the stored configuration. On failure
// it returns without touching the running state, so a transient error during
// re-initialisation leaves the previous bot working instead of none at all.
//
// A failed read of the stored configuration fails the load like every other
// subsystem. A missing telegram method or a stored configuration that does
// not decode is only logged: the bot is optional, so the panel starts, or
// keeps its running bot, without it. Failures talking to the Telegram API are
// only logged as well: the panel has to start and reload while Telegram is
// unreachable.
func Telegram(ctx context.Context, deps *Dependencies) error {
	log := logger.WithContext(ctx)
	method, err := findAuthMethod(ctx, deps, "telegram")
	if errors.Is(err, errAuthMethodMissing) {
		log.Errorw("[Init Telegram Config] no stored telegram auth method, running without a bot",
			logger.Field("method", "telegram"))
		return nil
	}
	if err != nil {
		log.Errorf("[Init Telegram Config] Get Telegram Config Error: %s", err.Error())
		return err
	}
	tgConfig := new(auth.TelegramAuthConfig)
	if err = tgConfig.Unmarshal(method.Config); err != nil {
		log.Errorw("[Init Telegram Config] stored auth method config is invalid, keeping the current bot",
			logger.Field("method", "telegram"), logger.Field("error", err.Error()))
		return nil
	}

	if tgConfig.BotToken == "" {
		// The bot is deliberately unconfigured: stop a leftover poller and
		// revoke every in-process reference to the previous client. Leaving the
		// old runtime snapshot or bot pointer published would let notification
		// paths keep sending with the token the administrator just cleared.
		swapTelegramPoller(nil)
		if deps.SetTelegramBot != nil {
			deps.SetTelegramBot(nil)
		}
		deps.updateRuntime(func(current *config.Runtime) { current.Telegram = config.Telegram{} })
		log.Debug("[Init Telegram Config] Telegram Token is empty")
		return nil
	}

	bot, err := tgbot.New(tgConfig.BotToken,
		// GetMe runs explicitly below so its result can feed the config.
		tgbot.WithSkipGetMe(),
		// The previous library processed updates strictly in order; handlers
		// rely on that for double-submit protection (bind tokens, admin
		// confirmations), so keep them synchronous.
		tgbot.WithNotAsyncHandlers(),
		// Only message updates are handled. Narrowing the subscription also
		// keeps unknown update payloads (which the library refuses to
		// decode) from ever reaching the polling loop.
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{models.AllowedUpdateMessage}),
		tgbot.WithErrorsHandler(func(err error) {
			logger.Error("[Telegram Bot] update transport error", logger.Field("error", err.Error()))
		}),
		tgbot.WithDefaultHandler(telegramUpdateHandler(deps)),
	)
	if err != nil {
		log.Error("[Init Telegram Config] New Bot API Error: ", logger.Field("error", err.Error()))
		return nil
	}

	// This runs synchronously inside startup and the admin settings request,
	// so it gets a deadline instead of the HTTP client's 60s default.
	getMeCtx, cancelGetMe := context.WithTimeout(ctx, 5*time.Second)
	user, err := bot.GetMe(getMeCtx)
	cancelGetMe()
	if err != nil {
		log.Error("[Init Telegram Config] Get Bot Info Error: ", logger.Field("error", err.Error()))
		return nil
	}

	// The group id is parsed leniently: a malformed value logs and behaves
	// like "no group configured" instead of failing the whole bot.
	var groupChatID int64
	if raw := strings.TrimSpace(tgConfig.GroupChatID); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			log.Errorf("[Init Telegram Config] Group chat id %q is not a number", raw)
		} else {
			groupChatID = id
		}
	}

	newConfig := config.Telegram{
		Enable:        authMethodEnabled(method),
		BotID:         user.ID,
		BotName:       user.Username,
		BotToken:      tgConfig.BotToken,
		EnableNotify:  tgConfig.EnableNotify,
		WebHookDomain: tgConfig.WebHookDomain,
		GroupChatID:   groupChatID,
	}

	// Pick mode: prefer webhook, fall back to long-polling when no domain or in debug.
	currentConfig := deps.currentConfig()
	useWebhook := tgConfig.WebHookDomain != "" && !currentConfig.Debug
	if useWebhook {
		// Webhook mode: register the URL with Telegram. The secret travels in
		// the X-Telegram-Bot-Api-Secret-Token header, never in the URL. The
		// config is published only after Telegram accepted the registration,
		// so a failure here cannot leave the handler expecting a secret
		// Telegram does not send yet.
		webhookURL := fmt.Sprintf("%s/v1/telegram/webhook", tgConfig.WebHookDomain)
		if _, err = bot.SetWebhook(ctx, &tgbot.SetWebhookParams{
			URL:         webhookURL,
			SecretToken: notification.WebhookSecret(tgConfig.BotToken),
			// One connection: Telegram then delivers the updates one at a
			// time, in order, as the long-polling loop does (see
			// WithNotAsyncHandlers above). The handlers' double-submit
			// protection (bind tokens, admin confirmations) is atomic on its
			// own, but a bot chat is not a workload that needs forty
			// concurrent deliveries either.
			MaxConnections: 1,
			AllowedUpdates: []string{models.AllowedUpdateMessage},
		}); err != nil {
			log.Errorf("[Init Telegram Config] Request Webhook Error: %s", err.Error())
			return nil
		}
		swapTelegramPoller(nil)
		deps.updateRuntime(func(current *config.Runtime) { current.Telegram = newConfig })
		if deps.SetTelegramBot != nil {
			deps.SetTelegramBot(bot)
		}
		log.Info("[Init Telegram Config] Webhook registered", logger.Field("url", webhookURL))
	} else {
		// Long-polling mode. A leftover webhook registration blocks
		// getUpdates, so drop it first.
		if _, err = bot.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{}); err != nil {
			log.Errorf("[Init Telegram Config] Delete Webhook Error: %s", err.Error())
		}
		// Publish the client and config before any update can arrive: the
		// update handlers reach the bot through the runtime-state accessor.
		deps.updateRuntime(func(current *config.Runtime) { current.Telegram = newConfig })
		if deps.SetTelegramBot != nil {
			deps.SetTelegramBot(bot)
		}
		swapTelegramPoller(func(ctx context.Context) { bot.Start(ctx) })
		mode := "long-polling"
		if currentConfig.Debug {
			mode = "long-polling (debug)"
		}
		log.Info("[Init Telegram Config] Using " + mode)
	}

	// Publish the command menu so the composer offers the bot's commands.
	// It is set on the bot itself, so it must be re-published whenever the
	// bot is (re)initialised. A failure is not fatal: the commands work
	// without a menu.
	if err := deps.Notification.PublishTelegramCommands(ctx); err != nil {
		log.Error("[Init Telegram Config] Publish Commands Error: ", logger.Field("error", err.Error()))
	}

	// The administrators' group hosts the notification topic, the support
	// and ticket topics, and the admin commands. When it is unusable, group
	// features switch off rather than degrade: the group is their only
	// channel by design.
	if groupChatID != 0 {
		if err := deps.Notification.SetupTelegramGroup(ctx); err != nil {
			log.Error("[Init Telegram Config] Admin group unusable, group features disabled",
				logger.Field("group_chat_id", groupChatID),
				logger.Field("error", err.Error()))
			deps.updateRuntime(func(current *config.Runtime) { current.Telegram.GroupChatID = 0 })
		} else {
			log.Info("[Init Telegram Config] Admin group ready", logger.Field("group_chat_id", groupChatID))
		}
	}

	log.Info("[Init Telegram Config] Telegram init success")
	return nil
}
