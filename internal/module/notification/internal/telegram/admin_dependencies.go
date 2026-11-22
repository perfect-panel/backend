package telegram

import (
	"context"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/pkg/logger"
)

// TelegramMessenger sends a response to a Telegram chat. A non-zero
// threadID addresses one forum topic inside a group; zero addresses the
// chat itself (private chats, or a group's General topic). Send delivers
// plain text; SendMarkdown delivers MarkdownV2 produced by RenderMarkdownV2,
// and must never receive unescaped dynamic data — Telegram rejects the
// whole message over one stray reserved character.
type TelegramMessenger interface {
	Send(ctx context.Context, chatID, threadID int64, message string) error
	SendMarkdown(ctx context.Context, chatID, threadID int64, message string) error
}

// TelegramAdminActionStore persists short-lived confirmations for destructive
// administrator commands. GetDel returns a key's value and removes the key in
// one step, so concurrent readers cannot both receive it; a missing key reads
// as redis.Nil.
type TelegramAdminActionStore interface {
	GetDel(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// AdminDependencies contains only the collaborators used by the
// administrator commands.
type AdminDependencies struct {
	Messenger     TelegramMessenger
	Actions       TelegramAdminActionStore
	Accounts      Accounts
	Tickets       Tickets
	Subscriptions Subscriptions
	Billing       Billing
	AuditLogs     AuditLogs
}

// Admin runs the administrator commands of the admin group, independently
// from the general update routing.
type Admin struct {
	deps AdminDependencies
}

func NewAdmin(deps AdminDependencies) *Admin {
	return &Admin{deps: deps}
}

// reply answers in the chat — and, inside the admin group, the same forum
// topic — the command came from. A failed delivery has nobody to tell but
// the log.
func (a *Admin) reply(ctx context.Context, msg *models.Message, message string) {
	if err := a.deps.Messenger.Send(ctx, msg.Chat.ID, int64(msg.MessageThreadID), message); err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] admin reply failed", logger.Field("error", err.Error()))
	}
}
