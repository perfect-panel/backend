package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/logger"
	"gorm.io/gorm"
)

// TicketTopicTitle names a ticket topic; the leading number keeps the topic
// list scannable next to /tk.
func TicketTopicTitle(t *ticket.Ticket) string {
	return fmt.Sprintf("🎫 #%d %s", t.Id, t.Title)
}

// telegramMessageLimit is Telegram's cap on a text message, in characters.
const telegramMessageLimit = 4096

// clampMessage shortens text to what Telegram accepts in one message, so a
// long website reply is mirrored cut rather than rejected whole.
func clampMessage(text string) string {
	r := []rune(text)
	if len(r) <= telegramMessageLimit {
		return text
	}
	return string(r[:telegramMessageLimit-1]) + "…"
}

// TicketCreated opens the ticket's topic and posts its opening message.
// Only tickets created after the group went live get a topic; older tickets
// have no mapping and their events are skipped upstream.
func (s *TopicService) TicketCreated(ctx context.Context, m TelegramMessenger, t *ticket.Ticket, userLabel string) error {
	topic, _, err := s.Ensure(ctx, telegramtopic.KindTicket, t.Id, TicketTopicTitle(t))
	if err != nil {
		return err
	}
	body := fmt.Sprintf("🎫 新工单 #%d\n用户：%s\n标题：%s", t.Id, userLabel, t.Title)
	if t.Description != "" {
		body += "\n\n" + t.Description
	}
	body += "\n\n直接在本话题回复即可答复用户；关闭话题即关闭工单。"
	_, err = s.PostText(ctx, m, topic, clampMessage(body))
	return err
}

// TicketReplied posts a website-side reply into the ticket's topic. A
// ticket without a mapping predates the group and is silently skipped.
func (s *TopicService) TicketReplied(ctx context.Context, m TelegramMessenger, ticketID int64, from, content string) error {
	topic, err := s.topics.FindByKindRef(ctx, s.group, telegramtopic.KindTicket, ticketID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Either the ticket predates the group, or its topic creation
			// failed and this ticket is invisible to the group — worth a
			// log line, not an error.
			logger.WithContext(ctx).Errorw("ticket has no forum topic, reply not mirrored", logger.Field("ticket_id", ticketID))
			return nil
		}
		return err
	}
	label := "💻 网站回复（管理员）"
	if ticket.IsFromUser(from) {
		label = "👤 用户回复"
	}
	_, err = s.PostText(ctx, m, topic, clampMessage(label+"：\n"+content))
	return err
}

// TicketStatusChanged mirrors a website-side status change onto the topic:
// closing the ticket closes the topic, any other status reopens it.
func (s *TopicService) TicketStatusChanged(ctx context.Context, ticketID int64, status uint8) error {
	topic, err := s.topics.FindByKindRef(ctx, s.group, telegramtopic.KindTicket, ticketID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if status == ticket.Closed {
		return s.Close(ctx, topic)
	}
	if topic.Status != telegramtopic.StatusActive {
		_, err = s.Reopen(ctx, topic)
		return err
	}
	return nil
}
