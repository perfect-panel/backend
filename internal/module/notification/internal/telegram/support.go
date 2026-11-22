package telegram

import (
	"context"
	"errors"
	"strconv"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/pkg/logger"
	"gorm.io/gorm"
)

// relaySupport forwards a bound user's private message into their live-chat
// topic in the admin group. Support deliberately requires a bound panel
// account: the topic is keyed by the panel user id, and staff see who they
// are talking to.
func (b *Bot) relaySupport(ctx context.Context, msg *models.Message) {
	group := b.groupChatID()
	if group == 0 || b.deps.Topics == nil || b.deps.TopicClient == nil {
		// Group features are off; a plain chat message keeps its historical
		// behaviour of being ignored.
		return
	}
	chatID := msg.Chat.ID
	log := logger.WithContext(ctx)

	auth, err := b.deps.Accounts.FindBinding(ctx, "telegram", strconv.FormatInt(chatID, 10))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			b.send(ctx, chatID, "请先绑定账号后再联系客服：登录面板 → 个人设置 → 绑定 Telegram。")
			return
		}
		log.Errorw("support relay: query binding failed", logger.Field("error", err.Error()), logger.Field("chat_id", chatID))
		return
	}

	if b.deps.Limiter != nil {
		allowed, shouldNotify := b.deps.Limiter.Allow(ctx, chatID)
		if !allowed {
			if shouldNotify {
				b.send(ctx, chatID, "消息发送过于频繁，请稍后再试。")
			}
			return
		}
	}

	topics := NewTopicService(b.deps.TopicClient, b.deps.Topics, group)
	topic, created, err := topics.Ensure(ctx, telegramtopic.KindSupport, auth.UserId, b.supportTopicTitle(ctx, auth.UserId, msg))
	if err != nil {
		log.Errorw("support relay: ensure topic failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		b.send(ctx, chatID, "客服暂时不可用，请稍后再试。")
		return
	}
	if created {
		b.send(ctx, chatID, "已为您接入人工客服，直接发送消息即可，客服会尽快回复。")
	}

	if _, err := topics.Relay(ctx, topic, func(threadID int64) error {
		return b.deps.TopicClient.ForwardToThread(ctx, group, threadID, chatID, msg.ID)
	}); err != nil {
		log.Errorw("support relay: forward failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		b.send(ctx, chatID, "消息转发失败，请稍后再试。")
	}
}

// supportTopicTitle names a live-chat topic after the panel account, with
// the Telegram username as a human-friendly hint.
func (b *Bot) supportTopicTitle(ctx context.Context, userID int64, msg *models.Message) string {
	label := "ID:" + strconv.FormatInt(userID, 10)
	if email, err := b.deps.Accounts.FindUserBinding(ctx, userID, "email"); err == nil && email.AuthIdentifier != "" {
		label = email.AuthIdentifier
	}
	title := "💬 " + label
	if msg.From != nil && msg.From.Username != "" {
		title += " · @" + msg.From.Username
	}
	return title
}
