// Package notification is the facade of the notification module's Telegram
// bot: update handling (webhook and polling), the administrators' group and
// its topics, user and administrator notices, the webhook secret and the
// message templates other domains render. The module's email and SMS
// delivery is not behind this facade: providers live in internal/infra/mail
// and internal/infra/sms, and the queue task handlers in
// internal/transport/task/email and internal/transport/task/sms send through
// them.
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/notification/internal/repo"
	"github.com/perfect-panel/backend/internal/module/notification/internal/telegram"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// HandleTelegramUpdate processes one bot update (command dispatch,
	// binding, admin actions). The polling loop calls it with the update the
	// bot library already decoded.
	HandleTelegramUpdate(ctx context.Context, update *models.Update)
	// HandleTelegramWebhook decodes one webhook payload and processes it.
	// The decode lives behind this facade so the bot library's update type
	// never crosses into the HTTP layer.
	HandleTelegramWebhook(ctx context.Context, payload []byte) error
	// NotifyTelegramUnbind sends the best-effort unbind notice to the chat.
	NotifyTelegramUnbind(ctx context.Context, userID, chatID int64) error
	// NotifyTelegramUser sends already-rendered MarkdownV2 text to the
	// user's bound Telegram chat; render it with RenderTelegramMarkdown so
	// the data is escaped. It reports an error when the user has no binding
	// or the bot is unconfigured, which callers treat as "nothing to
	// deliver".
	NotifyTelegramUser(ctx context.Context, userID int64, text string) error
	// PublishTelegramCommands registers the command menu every user sees.
	// The bot initialiser calls it once the client is ready, so the composer
	// offers the commands instead of leaving users to guess them.
	PublishTelegramCommands(ctx context.Context) error
	// SetupTelegramGroup validates the configured administrators' group
	// (forum supergroup, bot manages topics), prepares the notification
	// topic and publishes the group-scoped administrator menu. A zero group
	// id is a no-op; the caller disables group features when this errors.
	SetupTelegramGroup(ctx context.Context) error
	// NotifyAdminsTelegram posts already-rendered MarkdownV2 text (use
	// RenderTelegramMarkdown) into the administrators' notification topic.
	// It reports an error when the group is unavailable, which callers
	// treat as "nothing to deliver".
	NotifyAdminsTelegram(ctx context.Context, text string) error
	// NotifyTicketCreated opens a forum topic for a new website ticket.
	NotifyTicketCreated(ctx context.Context, t *ticket.Ticket) error
	// NotifyTicketReplied posts a website-side ticket reply into the
	// ticket's topic; tickets without a topic are skipped silently.
	NotifyTicketReplied(ctx context.Context, ticketID int64, from, content string) error
	// NotifyTicketStatusChanged mirrors a website-side status change onto
	// the ticket's topic (close/reopen).
	NotifyTicketStatusChanged(ctx context.Context, ticketID int64, status uint8) error
}

// RenderTelegramMarkdown renders one of the message templates below as
// MarkdownV2, escaping every data value. It is the only supported way to
// build the text for NotifyTelegramUser and the queue's bot sends: one
// unescaped '.' or '-' in an order number makes Telegram reject the whole
// message.
func RenderTelegramMarkdown(tpl string, data map[string]string) (string, error) {
	return telegram.RenderMarkdownV2(tpl, data)
}

// Message templates other domains render before handing the text to the bot.
const (
	PurchaseNotify        = telegram.PurchaseNotify
	RenewalNotify         = telegram.RenewalNotify
	ResetTrafficNotify    = telegram.ResetTrafficNotify
	RechargeNotify        = telegram.RechargeNotify
	AdminOrderNotify      = telegram.AdminOrderNotify
	AdminOrderDaily       = telegram.AdminOrderDaily
	SubscribeExpireNotify = telegram.SubscribeExpireNotify
)

// The bot's ports onto the domains it serves, declared by the bot and kept to
// the calls it makes; the composition root provides them.
type (
	Accounts      = telegram.Accounts
	Tickets       = telegram.Tickets
	Subscriptions = telegram.Subscriptions
	Billing       = telegram.Billing
	AuditLogs     = telegram.AuditLogs
)

// Deps declares everything the module needs; the composition root
// (internal/app) provides them.
type Deps struct {
	// Bot returns the current bot client; the initialize subsystem recreates
	// it when the Telegram configuration changes, so it is read per call.
	// nil means the bot is not configured.
	Bot func() *tgbot.Bot
	// GroupChatID returns the validated administrators' group chat id; zero
	// means no group is available and every group feature stays off. Read
	// per call for the same reason as Bot.
	GroupChatID func() int64
	// Topics maps forum topics in the administrators' group to the
	// conversation each carries.
	Topics        repository.TelegramTopicRepo
	Redis         *redis.Client
	Accounts      Accounts
	Tickets       Tickets
	Subscriptions Subscriptions
	Billing       Billing
	AuditLogs     AuditLogs
}

// New builds the notification module from its dependencies.
func New(deps Deps) Service {
	return &service{deps: deps}
}

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation).
func NewRepoBuilder() repository.NotificationBuilder {
	return func(c repository.ModuleConn) repository.NotificationRepos {
		return repository.NotificationRepos{
			TelegramTopics: repo.NewTelegramTopicRepo(c.DB),
		}
	}
}

type service struct {
	deps Deps
}

func (s *service) HandleTelegramWebhook(ctx context.Context, payload []byte) error {
	var update models.Update
	if err := json.Unmarshal(payload, &update); err != nil {
		return fmt.Errorf("decode telegram update: %w", err)
	}
	s.HandleTelegramUpdate(ctx, &update)
	return nil
}

func (s *service) HandleTelegramUpdate(ctx context.Context, update *models.Update) {
	// Without a client every adapter below would carry a nil bot that only
	// fails (with a panic) at send time; no bot means no updates to handle.
	bot := s.deps.Bot()
	if bot == nil {
		return
	}
	messenger := telegram.NewTelegramBotMessenger(bot)
	sessions := telegram.NewTelegramRedisStore(s.deps.Redis)
	admin := telegram.NewAdmin(telegram.AdminDependencies{
		Messenger:     messenger,
		Actions:       sessions,
		Accounts:      s.deps.Accounts,
		Tickets:       s.deps.Tickets,
		Subscriptions: s.deps.Subscriptions,
		Billing:       s.deps.Billing,
		AuditLogs:     s.deps.AuditLogs,
	})
	telegram.NewBot(telegram.BotDependencies{
		Messenger:     messenger,
		Sessions:      sessions,
		Accounts:      s.deps.Accounts,
		Subscriptions: s.deps.Subscriptions,
		Admin:         admin,
		GroupChatID:   s.deps.GroupChatID,
		Topics:        s.deps.Topics,
		TopicClient:   telegram.NewTelegramTopicClient(bot),
		Tickets:       s.deps.Tickets,
		Limiter:       sessions,
	}).HandleUpdate(ctx, update)
}

func (s *service) PublishTelegramCommands(ctx context.Context) error {
	bot := s.deps.Bot()
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	return telegram.NewTelegramBotCommandRegistrar(bot).
		SetCommands(ctx, 0, telegram.PublicCommands())
}

// topicService assembles the per-call topic layer; the bot client is read
// per call because re-initialisation replaces it.
func (s *service) topicService() (*telegram.TopicService, telegram.TelegramMessenger, error) {
	group := s.deps.GroupChatID()
	if group == 0 {
		return nil, nil, errors.New("telegram admin group is not configured")
	}
	bot := s.deps.Bot()
	if bot == nil {
		return nil, nil, errors.New("telegram bot is not configured")
	}
	topics := telegram.NewTopicService(telegram.NewTelegramTopicClient(bot), s.deps.Topics, group)
	return topics, telegram.NewTelegramBotMessenger(bot), nil
}

func (s *service) SetupTelegramGroup(ctx context.Context) error {
	if s.deps.GroupChatID() == 0 {
		return nil
	}
	bot := s.deps.Bot()
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	group := s.deps.GroupChatID()
	if err := telegram.NewTelegramTopicClient(bot).ValidateAdminGroup(ctx, group); err != nil {
		return err
	}
	topics, _, err := s.topicService()
	if err != nil {
		return err
	}
	if _, _, err := topics.Ensure(ctx, telegramtopic.KindNotify, 0, telegram.NotifyTopicTitle); err != nil {
		return err
	}
	// The menu is a convenience: the commands work without it.
	if err := telegram.NewTelegramBotCommandRegistrar(bot).
		SetGroupAdminCommands(ctx, group, telegram.AdminCommands()); err != nil {
		logger.WithContext(ctx).Error("[Telegram] publish group admin menu failed",
			logger.Field("error", err.Error()))
	}
	return nil
}

func (s *service) NotifyAdminsTelegram(ctx context.Context, text string) error {
	topics, messenger, err := s.topicService()
	if err != nil {
		return err
	}
	topic, _, err := topics.Ensure(ctx, telegramtopic.KindNotify, 0, telegram.NotifyTopicTitle)
	if err != nil {
		return err
	}
	_, err = topics.PostMarkdown(ctx, messenger, topic, text)
	return err
}

func (s *service) NotifyTicketCreated(ctx context.Context, t *ticket.Ticket) error {
	topics, messenger, err := s.topicService()
	if err != nil {
		return err
	}
	return topics.TicketCreated(ctx, messenger, t, s.userLabel(ctx, t.UserId))
}

func (s *service) NotifyTicketReplied(ctx context.Context, ticketID int64, from, content string) error {
	topics, messenger, err := s.topicService()
	if err != nil {
		return err
	}
	return topics.TicketReplied(ctx, messenger, ticketID, from, content)
}

func (s *service) NotifyTicketStatusChanged(ctx context.Context, ticketID int64, status uint8) error {
	topics, _, err := s.topicService()
	if err != nil {
		return err
	}
	return topics.TicketStatusChanged(ctx, ticketID, status)
}

// userLabel names a user for staff-facing text: the email when bound, the
// numeric id otherwise.
func (s *service) userLabel(ctx context.Context, userID int64) string {
	if method, err := s.deps.Accounts.FindUserBinding(ctx, userID, "email"); err == nil && method.AuthIdentifier != "" {
		return method.AuthIdentifier
	}
	return fmt.Sprintf("ID:%d", userID)
}

func (s *service) NotifyTelegramUser(ctx context.Context, userID int64, text string) error {
	bot := s.deps.Bot()
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	method, err := s.deps.Accounts.FindUserBinding(ctx, userID, "telegram")
	if err != nil {
		return err
	}
	chatID, err := strconv.ParseInt(method.AuthIdentifier, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram chat id %q is malformed: %w", method.AuthIdentifier, err)
	}
	_, err = bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	})
	return err
}

func (s *service) NotifyTelegramUnbind(ctx context.Context, userID, chatID int64) error {
	text, err := telegram.RenderMarkdownV2(telegram.UnbindNotify, map[string]string{
		"Id":   strconv.FormatInt(userID, 10),
		"Time": timeutil.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return err
	}
	bot := s.deps.Bot()
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	_, err = bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	})
	return err
}
