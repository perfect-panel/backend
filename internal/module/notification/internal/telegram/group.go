package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/logger"
	"gorm.io/gorm"
)

// handleGroup processes one message inside the admin group: topic lifecycle
// service messages, administrator commands, and replies inside support or
// ticket topics.
func (b *Bot) handleGroup(ctx context.Context, msg *models.Message) {
	if msg.ForumTopicClosed != nil || msg.ForumTopicReopened != nil {
		b.syncTopicLifecycle(ctx, msg)
		return
	}
	if msg.From == nil || msg.From.IsBot {
		return
	}
	if cmd := messageCommand(msg); cmd != "" {
		if isAdminCommand(cmd) {
			b.deps.Admin.Handle(ctx, msg)
		}
		return
	}
	if msg.MessageThreadID == 0 || b.deps.Topics == nil {
		return
	}
	topic, err := b.deps.Topics.FindByThread(ctx, msg.Chat.ID, int64(msg.MessageThreadID))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.WithContext(ctx).Errorw("group relay: topic lookup failed", logger.Field("error", err.Error()))
		}
		return
	}
	// Only support and ticket topics carry a user conversation; chatter in
	// the notification feed (or any future kind) is none of the bot's
	// business.
	if topic.Kind != telegramtopic.KindSupport && topic.Kind != telegramtopic.KindTicket {
		return
	}
	// Whatever staff write in a mapped topic reaches a customer, so it
	// carries the same authority as an administrator command.
	if reject := b.rejectNonAdminSender(ctx, msg, "回复未送达用户"); reject != "" {
		b.rejectAloud(ctx, msg, reject)
		return
	}
	switch topic.Kind {
	case telegramtopic.KindSupport:
		b.relayAdminReply(ctx, msg, topic)
	case telegramtopic.KindTicket:
		b.appendTicketFollow(ctx, msg, topic)
	}
}

// rejectNonAdminSender enforces panel-administrator identity for actions
// taken inside mapped topics: the sender must have their own Telegram bound
// to an enabled, non-deleted administrator account. The sender would
// otherwise reasonably believe the action took effect, so a rejection is
// always spoken, never silent; consequence names what did not happen.
func (b *Bot) rejectNonAdminSender(ctx context.Context, msg *models.Message, consequence string) string {
	if b.deps.Accounts == nil {
		return "系统未配置，" + consequence + "。"
	}
	if msg.From == nil {
		return "⚠️ 无法识别发送者，" + consequence + "。"
	}
	log := logger.WithContext(ctx)
	auth, err := b.deps.Accounts.FindBinding(ctx, "telegram", strconv.FormatInt(msg.From.ID, 10))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "⚠️ 您的 Telegram 未绑定管理员账号，" + consequence + "。"
		}
		log.Errorw("group relay: sender lookup failed", logger.Field("error", err.Error()))
		return "系统错误，" + consequence + "。"
	}
	u, err := b.deps.Accounts.FindUser(ctx, auth.UserId)
	if err != nil {
		log.Errorw("group relay: sender user lookup failed", logger.Field("error", err.Error()))
		return "系统错误，" + consequence + "。"
	}
	if refusal := panelAdminRefusal(u); refusal != "" {
		log.Infow("group relay: sender may not administer", logger.Field("user_id", u.Id), logger.Field("reason", refusal))
		return "⚠️ 您不是管理员，" + consequence + "。"
	}
	return ""
}

// rejectAloud posts a rejection into the topic the sender acted in. It is
// rate-limited per sender: an unbound member flooding a topic must not make
// the bot flood it too.
func (b *Bot) rejectAloud(ctx context.Context, msg *models.Message, reject string) {
	if b.deps.Limiter != nil && msg.From != nil {
		if allowed, _ := b.deps.Limiter.Allow(ctx, msg.From.ID); !allowed {
			return
		}
	}
	b.sendToTopic(ctx, msg, reject)
}

// sendToTopic answers inside the topic msg was posted in.
func (b *Bot) sendToTopic(ctx context.Context, msg *models.Message, text string) {
	if err := b.deps.Messenger.Send(ctx, msg.Chat.ID, int64(msg.MessageThreadID), text); err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] send topic message failed", logger.Field("error", err.Error()))
	}
}

// relayAdminReply copies a staff message from a support topic to the bound
// user's private chat, hiding which administrator wrote it.
func (b *Bot) relayAdminReply(ctx context.Context, msg *models.Message, topic *telegramtopic.Topic) {
	method, err := b.deps.Accounts.FindUserBinding(ctx, topic.RefId, "telegram")
	if err != nil {
		b.sendToTopic(ctx, msg, "⚠️ 该用户已解绑 Telegram，消息无法送达。")
		return
	}
	customerChat, err := strconv.ParseInt(method.AuthIdentifier, 10, 64)
	if err != nil {
		logger.WithContext(ctx).Errorw("support relay: malformed chat id", logger.Field("value", method.AuthIdentifier))
		return
	}
	if err := b.deps.TopicClient.CopyTo(ctx, customerChat, msg.Chat.ID, msg.ID); err != nil {
		logger.WithContext(ctx).Errorw("support relay: copy to user failed", logger.Field("error", err.Error()), logger.Field("user_id", topic.RefId))
		b.sendToTopic(ctx, msg, "⚠️ 回复送达失败，用户可能已停用 Bot。")
	}
}

// appendTicketFollow records a staff message in a ticket topic as an
// administrator reply, exactly like the /rp command, so the website thread
// and the topic stay one conversation. The topic already shows the message,
// so the reply is not mirrored back into it.
func (b *Bot) appendTicketFollow(ctx context.Context, msg *models.Message, topic *telegramtopic.Topic) {
	if b.deps.Tickets == nil {
		return
	}
	if msg.Text == "" {
		b.sendToTopic(ctx, msg, "工单回复目前仅支持文本消息。")
		return
	}
	if _, err := b.deps.Tickets.Reply(ctx, topic.RefId, staffAuthor, msg.Text, true); err != nil {
		if errors.Is(err, ticket.ErrClosed) {
			b.sendToTopic(ctx, msg, fmt.Sprintf("⚠️ 工单 #%d 已关闭，回复未保存。请先 /reopen_%d 重新打开工单。", topic.RefId, topic.RefId))
			return
		}
		logger.WithContext(ctx).Errorw("ticket relay: reply failed", logger.Field("error", err.Error()), logger.Field("ticket_id", topic.RefId))
		b.sendToTopic(ctx, msg, "⚠️ 回复保存失败，请稍后再试。")
	}
}

// syncTopicLifecycle mirrors a HUMAN closing or reopening a topic inside
// Telegram back onto the mapping and, for ticket topics, the ticket itself.
// The bot's own Close/Reopen calls emit the same service messages (with the
// bot as From) and must be ignored: syncing them back would overwrite the
// status the website side just wrote — e.g. an automatic reopen while
// posting a website reply would flip the ticket from Waiting to Pending.
//
// Any group member with the manage-topics right can close a topic, so the
// effects beyond the mapping — the ticket status and the customer notice —
// require the same panel-administrator identity as a topic reply.
func (b *Bot) syncTopicLifecycle(ctx context.Context, msg *models.Message) {
	if msg.From != nil && msg.From.IsBot {
		return
	}
	if msg.MessageThreadID == 0 || b.deps.Topics == nil {
		return
	}
	log := logger.WithContext(ctx)
	topic, err := b.deps.Topics.FindByThread(ctx, msg.Chat.ID, int64(msg.MessageThreadID))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorw("topic lifecycle: topic lookup failed", logger.Field("error", err.Error()))
		}
		return
	}
	closed := msg.ForumTopicClosed != nil
	status := uint8(telegramtopic.StatusActive)
	if closed {
		status = telegramtopic.StatusClosed
	}
	// The mapping records the topic's actual state in Telegram, whoever
	// changed it, so the relays keep reopening it when needed.
	if err := b.deps.Topics.UpdateStatus(ctx, topic.Id, status); err != nil {
		log.Errorw("topic lifecycle: mapping update failed", logger.Field("error", err.Error()))
	}
	switch {
	case topic.Kind == telegramtopic.KindTicket && b.deps.Tickets != nil:
		if reject := b.rejectNonAdminSender(ctx, msg, "工单状态未同步"); reject != "" {
			b.rejectAloud(ctx, msg, reject)
			return
		}
		ticketStatus := uint8(ticket.Pending)
		if closed {
			ticketStatus = ticket.Closed
		}
		// The topic already is in the new state; mirroring the change back
		// would only race a quick close-and-reopen.
		if err := b.deps.Tickets.SetStatus(ctx, topic.RefId, ticketStatus, true); err != nil {
			log.Errorw("topic lifecycle: ticket status sync failed", logger.Field("error", err.Error()), logger.Field("ticket_id", topic.RefId))
		}
	case topic.Kind == telegramtopic.KindSupport && closed:
		if reject := b.rejectNonAdminSender(ctx, msg, "用户未收到会话结束通知"); reject != "" {
			b.rejectAloud(ctx, msg, reject)
			return
		}
		// Best effort: tell the customer the conversation ended.
		if method, err := b.deps.Accounts.FindUserBinding(ctx, topic.RefId, "telegram"); err == nil {
			if chatID, perr := strconv.ParseInt(method.AuthIdentifier, 10, 64); perr == nil {
				b.send(ctx, chatID, "本次客服会话已结束。如需帮助，直接发送消息即可重新开启。")
			}
		}
	}
}
