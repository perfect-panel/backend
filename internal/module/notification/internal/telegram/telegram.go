// Package telegram implements the notification module's Telegram bot: the
// routing of updates, account binding and the traffic report in private
// chats, the administrator commands and the support and ticket topics of
// the administrators' group, and the MarkdownV2 message templates. Only the
// module facade may reach it.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Bot routes Telegram updates. It holds no per-update state: every method
// takes the context of the update it serves.
type Bot struct {
	deps BotDependencies
}

// NewBot builds the update router; the group dependencies may be left zero,
// which turns every group feature off.
func NewBot(deps BotDependencies) *Bot {
	return &Bot{deps: deps}
}

// HandleUpdate routes one update. User commands live in the private chat;
// everything administrative — commands, support topics, ticket topics —
// lives in the configured admin group. Messages from other groups are
// ignored entirely.
func (b *Bot) HandleUpdate(ctx context.Context, req *models.Update) {
	msg := req.Message
	if msg == nil {
		return
	}
	group := b.groupChatID()
	switch {
	case msg.Chat.Type == models.ChatTypePrivate:
		b.handlePrivate(ctx, msg)
	case group != 0 && msg.Chat.ID == group:
		b.handleGroup(ctx, msg)
	}
}

func (b *Bot) groupChatID() int64 {
	if b.deps.GroupChatID == nil {
		return 0
	}
	return b.deps.GroupChatID()
}

// privateHelp answers /help in the private chat.
const privateHelp = "🤖 可用命令：\n/start <令牌> 或 /bind <令牌> —— 绑定面板账号\n/traffic —— 查看订阅流量\n\n绑定后直接发送消息即可联系人工客服。"

func (b *Bot) handlePrivate(ctx context.Context, msg *models.Message) {
	if msg.From != nil && msg.From.IsBot {
		return
	}
	cmd := messageCommand(msg)
	// /help is in the public menu, so in the private chat it must answer
	// with the user-facing help — the admin help lives in the group.
	if cmd == "help" || cmd == "h" {
		b.send(ctx, msg.Chat.ID, privateHelp)
		return
	}
	if isAdminCommand(cmd) {
		b.send(ctx, msg.Chat.ID, "管理员命令只能在管理群中使用。")
		return
	}
	switch cmd {
	case "traffic":
		b.traffic(ctx, msg.Chat.ID)
	case "bind":
		b.bind(ctx, msg.Chat.ID, commandArguments(msg), "Please provide a bind token. Usage: /bind <token>")
	case "start":
		// /start without a token is a user opening the bot rather than
		// following a panel deep link.
		b.bind(ctx, msg.Chat.ID, commandArguments(msg), "Please bind account!")
	case "":
		// A plain message is a support request: relay it into the user's
		// live-chat topic in the admin group.
		b.relaySupport(ctx, msg)
	}
}

func isAdminCommand(cmd string) bool {
	cmd, _ = expandShortcut(cmd, "")
	switch cmd {
	case "dash", "tickets", "tickets_waiting", "tk", "rp", "close", "reopen",
		"user", "user_sub", "user_log", "reset", "toggle", "ban", "help", "h":
		return true
	}
	if strings.HasPrefix(cmd, "confirm_") || strings.HasPrefix(cmd, "cancel_") {
		return true
	}
	return false
}

// send delivers plain text to a chat. The chat is the only one to tell about
// a failed delivery, so the failure goes to the log.
func (b *Bot) send(ctx context.Context, chatID int64, message string) {
	if err := b.deps.Messenger.Send(ctx, chatID, 0, message); err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] send message failed", logger.Field("error", err.Error()))
	}
}

func (b *Bot) sendMarkdown(ctx context.Context, chatID int64, message string) {
	if err := b.deps.Messenger.SendMarkdown(ctx, chatID, 0, message); err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] send message failed", logger.Field("error", err.Error()))
	}
}

type telegramBotMessenger struct {
	bot *tgbot.Bot
}

// Send delivers plain text: command replies and administrator output carry
// no formatting, and plain text cannot be broken by the data inside it.
func (m telegramBotMessenger) Send(ctx context.Context, chatID, threadID int64, message string) error {
	_, err := m.bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: int(threadID),
		Text:            message,
	})
	return err
}

// SendMarkdown delivers MarkdownV2 built by RenderMarkdownV2.
func (m telegramBotMessenger) SendMarkdown(ctx context.Context, chatID, threadID int64, message string) error {
	_, err := m.bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:          chatID,
		MessageThreadID: int(threadID),
		Text:            message,
		ParseMode:       models.ParseModeMarkdown,
	})
	return err
}

func botCommands(commands []Command) []models.BotCommand {
	botCommands := make([]models.BotCommand, 0, len(commands))
	for _, command := range commands {
		botCommands = append(botCommands, models.BotCommand{
			Command:     command.Command,
			Description: command.Description,
		})
	}
	return botCommands
}

// SetCommands publishes a command menu. A zero chatID targets the default
// scope every user sees; otherwise the menu applies to that chat alone, which
// is how administrator commands stay hidden from ordinary users.
func (m telegramBotMessenger) SetCommands(ctx context.Context, chatID int64, commands []Command) error {
	params := &tgbot.SetMyCommandsParams{Commands: botCommands(commands)}
	if chatID != 0 {
		params.Scope = &models.BotCommandScopeChat{ChatID: chatID}
	}
	_, err := m.bot.SetMyCommands(ctx, params)
	return err
}

// SetGroupAdminCommands publishes a menu that only the group's
// administrators see in their composer.
func (m telegramBotMessenger) SetGroupAdminCommands(ctx context.Context, chatID int64, commands []Command) error {
	_, err := m.bot.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: botCommands(commands),
		Scope:    &models.BotCommandScopeChatAdministrators{ChatID: chatID},
	})
	return err
}

// traffic answers /traffic with the traffic of the bound account's servable
// subscriptions. The binding alone says nothing about the account: a
// disabled or deleted one is refused, as the panel refuses it everywhere.
func (b *Bot) traffic(ctx context.Context, chatID int64) {
	log := logger.WithContext(ctx)
	auth, err := b.deps.Accounts.FindBinding(ctx, "telegram", strconv.FormatInt(chatID, 10))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			b.send(ctx, chatID, "请先绑定账号：登录面板 → 个人设置 → 绑定 Telegram。")
			return
		}
		log.Errorw("[Telegram] traffic: query binding failed", logger.Field("error", err.Error()))
		b.send(ctx, chatID, "查询失败，请稍后再试。")
		return
	}
	u, err := b.deps.Accounts.FindUser(ctx, auth.UserId)
	if err != nil {
		log.Errorw("[Telegram] traffic: query user failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		b.send(ctx, chatID, "查询失败，请稍后再试。")
		return
	}
	if refusal := accountRefusal(u); refusal != "" {
		log.Infow("[Telegram] traffic: account refused", logger.Field("user_id", u.Id), logger.Field("reason", refusal))
		b.send(ctx, chatID, "您的账号已停用，无法查询订阅流量。")
		return
	}
	subs, err := b.deps.Subscriptions.ListByUser(ctx, auth.UserId)
	if err != nil {
		log.Errorw("[Telegram] traffic: list subscriptions failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		b.send(ctx, chatID, "查询失败，请稍后再试。")
		return
	}
	b.send(ctx, chatID, trafficReport(subs, timeutil.Now()))
}

// trafficReport renders the usage of the subscriptions that can be served at
// now, by the rule the node user list applies (usersub.AvailabilityAt): a
// live status, a term still running and traffic left. A zero quota is
// unlimited traffic, and no expiry (usersub.NoExpiry) is no time limit, as
// everywhere else in the panel.
func trafficReport(subs []*usersub.SubscribeDetails, now time.Time) string {
	var sb strings.Builder
	for _, s := range subs {
		if availabilityAt(s, now) != usersub.Available {
			continue
		}
		if sb.Len() == 0 {
			sb.WriteString("📊 订阅流量\n━━━━━━━━━━━━━━━━━━\n")
		}
		used := s.Download + s.Upload
		quota := "无限制"
		remaining := "无限制"
		if s.Traffic > 0 {
			quota = trafficGB(s.Traffic)
			remaining = trafficGB(s.Traffic - used)
		}
		expiry := "无限期"
		if !usersub.NoExpiry(s.ExpireTime) {
			expiry = s.ExpireTime.In(timeutil.Location()).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&sb, "📦 %s\n   已用：%s / %s\n   剩余：%s\n   到期：%s\n", planName(s), trafficGB(used), quota, remaining, expiry)
	}
	if sb.Len() == 0 {
		return "您当前没有生效中的订阅。"
	}
	return sb.String()
}

// availabilityAt classifies a listed subscription by the entity's rule; the
// details row carries the subscription's own columns.
func availabilityAt(s *usersub.SubscribeDetails, now time.Time) usersub.Availability {
	sub := usersub.Subscribe{
		Status: s.Status, StartTime: s.StartTime, ExpireTime: s.ExpireTime, FinishedAt: s.FinishedAt,
		Traffic: s.Traffic, Download: s.Download, Upload: s.Upload,
	}
	return sub.AvailabilityAt(now)
}

// bindTokenKey addresses a single-use account-binding token. Binding tokens
// live under their own prefix: the account's session id must never double as
// a binding capability, because the deep link carrying it is shared through
// Telegram chats.
func bindTokenKey(token string) string {
	return fmt.Sprintf("%v:%v", config.TelegramBindKey, token)
}

// bindLockKey addresses the lock one account's bindings are made under.
func bindLockKey(userID int64) string {
	return fmt.Sprintf("%v:lock:%d", config.TelegramBindKey, userID)
}

// bindLockTTL bounds the binding lock: long enough for the checks and the
// insert, short enough that a crashed redemption does not lock the account
// out of binding for long.
const bindLockTTL = 10 * time.Second

// bindFailed answers every bind failure the user cannot fix themselves.
const bindFailed = "Bind failed. Please try again later."

// bind redeems a single-use binding token issued by the panel, binding the
// chat to the panel account it names. /start (the deep link) and /bind (the
// manual command) differ only in the prompt for a missing token.
//
// The token is consumed before anything else, in one step: in webhook mode
// updates are handled concurrently, so two chats opening the same forwarded
// link at once must not both find the token. A redemption that then fails
// for a reason the user can fix, or a failure of the stores, puts the token
// back with the life it had left, so the user can retry without a new link
// and nobody can keep a leaked link alive by failing on purpose. The
// binding itself runs under a lock on the account, so two links of one
// account redeemed at once cannot both pass the "not yet bound" check.
func (b *Bot) bind(ctx context.Context, chatID int64, token, missingToken string) {
	if token == "" {
		b.send(ctx, chatID, missingToken)
		return
	}
	log := logger.WithContext(ctx)
	key := bindTokenKey(token)
	value, ttl, err := b.deps.Sessions.Take(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Infow("[Telegram] bind: token not found or expired")
			b.send(ctx, chatID, "Bind token is invalid or expired. Please request a new one.")
			return
		}
		log.Errorw("[Telegram] bind: read token failed", logger.Field("error", err.Error()))
		b.send(ctx, chatID, bindFailed)
		return
	}
	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		log.Errorw("[Telegram] bind: malformed token value", logger.Field("error", err.Error()))
		b.send(ctx, chatID, "Bind failed. Invalid session data.")
		return
	}
	// restore puts the unredeemed token back for the time it had left.
	restore := func() {
		if ttl <= 0 {
			return
		}
		if err := b.deps.Sessions.Set(ctx, key, value, ttl); err != nil {
			log.Errorw("[Telegram] bind: restore token failed", logger.Field("error", err.Error()))
		}
	}
	chatIDStr := strconv.FormatInt(chatID, 10)

	// One Telegram account binds one panel account...
	byChat, err := b.deps.Accounts.FindBinding(ctx, "telegram", chatIDStr)
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		log.Errorw("[Telegram] bind: query chat binding failed", logger.Field("error", err.Error()), logger.Field("chat_id", chatID))
		restore()
		b.send(ctx, chatID, bindFailed)
		return
	case err == nil && byChat.Id > 0 && byChat.UserId != userID:
		log.Infow("[Telegram] bind: chat already bound to another user",
			logger.Field("chat_id", chatID), logger.Field("existing_user_id", byChat.UserId), logger.Field("user_id", userID))
		restore()
		b.send(ctx, chatID, "This Telegram account is already bound to another user.")
		return
	}

	lock := bindLockKey(userID)
	locked, err := b.deps.Sessions.Acquire(ctx, lock, bindLockTTL)
	if err != nil || !locked {
		if err != nil {
			log.Errorw("[Telegram] bind: acquire binding lock failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
		} else {
			log.Infow("[Telegram] bind: another redemption for the account is in progress", logger.Field("user_id", userID))
		}
		restore()
		b.send(ctx, chatID, bindFailed)
		return
	}
	defer func() {
		if err := b.deps.Sessions.Delete(ctx, lock); err != nil {
			log.Errorw("[Telegram] bind: release binding lock failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
		}
	}()

	// ...and one panel account one Telegram account; an existing binding is
	// never overwritten silently. The check runs under the lock, so a
	// binding another redemption is making right now is seen.
	byUser, err := b.deps.Accounts.FindUserBinding(ctx, userID, "telegram")
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		log.Errorw("[Telegram] bind: query user binding failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
		restore()
		b.send(ctx, chatID, bindFailed)
		return
	case err == nil && byUser.Id > 0 && byUser.AuthIdentifier == chatIDStr:
		// Bound already: the token has done its work and stays consumed.
		b.send(ctx, chatID, "This account is already bound to your Telegram.")
		return
	case err == nil && byUser.Id > 0:
		log.Infow("[Telegram] bind: user already bound to a different chat",
			logger.Field("user_id", userID), logger.Field("existing_chat_id", byUser.AuthIdentifier), logger.Field("chat_id", chatID))
		restore()
		b.send(ctx, chatID, "Your account is already bound to a different Telegram account. Please unbind it first.")
		return
	}

	if err := b.deps.Accounts.BindTelegram(ctx, userID, chatIDStr); err != nil {
		log.Errorw("[Telegram] bind: insert binding failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
		restore()
		b.send(ctx, chatID, bindFailed)
		return
	}

	text, err := RenderMarkdownV2(BindNotify, map[string]string{
		"Id":   strconv.FormatInt(userID, 10),
		"Time": timeutil.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		log.Errorw("[Telegram] bind: render notice failed", logger.Field("error", err.Error()))
		b.send(ctx, chatID, "Bound successfully!")
		return
	}
	b.sendMarkdown(ctx, chatID, text)
}
