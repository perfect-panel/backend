package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"gorm.io/gorm"
)

// NotifyTopicTitle names the operations feed topic the bot creates in the
// administrators' group.
const NotifyTopicTitle = "📣 运营通知"

// TelegramTopicClient is the group-side Bot API surface the topic layer
// drives. telegramBotMessenger implements it; tests fake it.
type TelegramTopicClient interface {
	// ValidateAdminGroup confirms the chat is a forum-enabled supergroup
	// where the bot may manage topics.
	ValidateAdminGroup(ctx context.Context, chatID int64) error
	CreateTopic(ctx context.Context, chatID int64, name string) (int64, error)
	DeleteTopic(ctx context.Context, chatID, threadID int64) error
	CloseTopic(ctx context.Context, chatID, threadID int64) error
	ReopenTopic(ctx context.Context, chatID, threadID int64) error
	// ForwardToThread relays a user's message into a group topic keeping
	// the sender identity visible to the support staff.
	ForwardToThread(ctx context.Context, chatID, threadID, fromChatID int64, messageID int) error
	// CopyTo relays a group message to a private chat without exposing
	// which administrator wrote it.
	CopyTo(ctx context.Context, toChatID, fromChatID int64, messageID int) error
}

// NewTelegramTopicClient adapts the bot client to the topic-management port.
func NewTelegramTopicClient(bot *tgbot.Bot) TelegramTopicClient {
	return telegramBotMessenger{bot: bot}
}

func (m telegramBotMessenger) ValidateAdminGroup(ctx context.Context, chatID int64) error {
	chat, err := m.bot.GetChat(ctx, &tgbot.GetChatParams{ChatID: chatID})
	if err != nil {
		return fmt.Errorf("get chat: %w", err)
	}
	if chat.Type != models.ChatTypeSupergroup || !chat.IsForum {
		return errors.New("the chat is not a supergroup with topics enabled")
	}
	member, err := m.bot.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: chatID, UserID: m.bot.ID()})
	if err != nil {
		return fmt.Errorf("get bot membership: %w", err)
	}
	switch {
	case member.Owner != nil:
	case member.Administrator != nil && member.Administrator.CanManageTopics:
	default:
		return errors.New("the bot must be a group administrator with the manage-topics right")
	}
	return nil
}

func (m telegramBotMessenger) CreateTopic(ctx context.Context, chatID int64, name string) (int64, error) {
	topic, err := m.bot.CreateForumTopic(ctx, &tgbot.CreateForumTopicParams{ChatID: chatID, Name: name})
	if err != nil {
		return 0, err
	}
	return int64(topic.MessageThreadID), nil
}

func (m telegramBotMessenger) DeleteTopic(ctx context.Context, chatID, threadID int64) error {
	_, err := m.bot.DeleteForumTopic(ctx, &tgbot.DeleteForumTopicParams{ChatID: chatID, MessageThreadID: int(threadID)})
	return err
}

func (m telegramBotMessenger) CloseTopic(ctx context.Context, chatID, threadID int64) error {
	_, err := m.bot.CloseForumTopic(ctx, &tgbot.CloseForumTopicParams{ChatID: chatID, MessageThreadID: int(threadID)})
	return err
}

func (m telegramBotMessenger) ReopenTopic(ctx context.Context, chatID, threadID int64) error {
	_, err := m.bot.ReopenForumTopic(ctx, &tgbot.ReopenForumTopicParams{ChatID: chatID, MessageThreadID: int(threadID)})
	return err
}

func (m telegramBotMessenger) ForwardToThread(ctx context.Context, chatID, threadID, fromChatID int64, messageID int) error {
	_, err := m.bot.ForwardMessage(ctx, &tgbot.ForwardMessageParams{
		ChatID:          chatID,
		MessageThreadID: int(threadID),
		FromChatID:      fromChatID,
		MessageID:       messageID,
	})
	return err
}

func (m telegramBotMessenger) CopyTo(ctx context.Context, toChatID, fromChatID int64, messageID int) error {
	_, err := m.bot.CopyMessage(ctx, &tgbot.CopyMessageParams{
		ChatID:     toChatID,
		FromChatID: fromChatID,
		MessageID:  messageID,
	})
	return err
}

// isMissingThreadError classifies the Bot API rejection for a forum topic
// that an administrator deleted; the mapping self-heals by recreating it.
func isMissingThreadError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "message thread not found") || strings.Contains(msg, "topic_deleted")
}

// isTopicClosedError classifies the rejection for posting into a closed
// topic; the sender reopens the topic and retries.
func isTopicClosedError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "topic_closed")
}

// TopicService owns the mapping between the forum topics of one
// administrators' group and the conversation each carries. It is built per
// call: the group and the bot client can change with the configuration.
type TopicService struct {
	client TelegramTopicClient
	topics repository.TelegramTopicRepo
	group  int64
}

// NewTopicService builds the topic layer of the administrators' group.
func NewTopicService(client TelegramTopicClient, topics repository.TelegramTopicRepo, group int64) *TopicService {
	return &TopicService{client: client, topics: topics, group: group}
}

// topicTitleLimit is Telegram's cap on forum topic names.
const topicTitleLimit = 128

func clampTopicTitle(title string) string {
	r := []rune(title)
	if len(r) <= topicTitleLimit {
		return title
	}
	return string(r[:topicTitleLimit-1]) + "…"
}

// Ensure returns the mapping for (kind, ref), creating the forum topic on
// first use; created reports whether this call made it. Concurrent creators
// race on the unique key; the loser adopts the winner's row.
func (s *TopicService) Ensure(ctx context.Context, kind uint8, refID int64, title string) (topic *telegramtopic.Topic, created bool, err error) {
	topic, err = s.topics.FindByKindRef(ctx, s.group, kind, refID)
	if err == nil {
		return topic, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	title = clampTopicTitle(title)
	threadID, err := s.client.CreateTopic(ctx, s.group, title)
	if err != nil {
		return nil, false, fmt.Errorf("create forum topic: %w", err)
	}
	row := &telegramtopic.Topic{
		ChatId:   s.group,
		Kind:     kind,
		RefId:    refID,
		ThreadId: threadID,
		Title:    title,
		Status:   telegramtopic.StatusActive,
	}
	if err := s.topics.Insert(ctx, row); err != nil {
		// The unique key lost a race: adopt the winner's mapping. The topic
		// this call created is deleted (best effort) — an orphaned topic
		// would let staff reply into a thread that reaches nobody.
		if existing, ferr := s.topics.FindByKindRef(ctx, s.group, kind, refID); ferr == nil {
			if derr := s.client.DeleteTopic(ctx, s.group, threadID); derr != nil {
				logger.WithContext(ctx).Errorw("orphaned forum topic could not be deleted",
					logger.Field("error", derr.Error()), logger.Field("thread_id", threadID))
			}
			return existing, false, nil
		}
		return nil, false, err
	}
	return row, true, nil
}

// Recreate repoints a mapping whose topic was deleted inside Telegram.
func (s *TopicService) Recreate(ctx context.Context, topic *telegramtopic.Topic) (*telegramtopic.Topic, error) {
	threadID, err := s.client.CreateTopic(ctx, s.group, topic.Title)
	if err != nil {
		return nil, fmt.Errorf("recreate forum topic: %w", err)
	}
	if err := s.topics.UpdateThread(ctx, topic.Id, threadID); err != nil {
		return nil, err
	}
	updated := *topic
	updated.ThreadId = threadID
	updated.Status = telegramtopic.StatusActive
	return &updated, nil
}

// Reopen reopens a closed topic and marks the mapping active. A missing
// topic is recreated instead.
func (s *TopicService) Reopen(ctx context.Context, topic *telegramtopic.Topic) (*telegramtopic.Topic, error) {
	if err := s.client.ReopenTopic(ctx, s.group, topic.ThreadId); err != nil && !isTopicNotModifiedError(err) {
		if isMissingThreadError(err) {
			return s.Recreate(ctx, topic)
		}
		return nil, err
	}
	if topic.Status != telegramtopic.StatusActive {
		if err := s.topics.UpdateStatus(ctx, topic.Id, telegramtopic.StatusActive); err != nil {
			return nil, err
		}
	}
	updated := *topic
	updated.Status = telegramtopic.StatusActive
	return &updated, nil
}

// Close closes the forum topic and marks the mapping; a topic already gone
// from Telegram still gets its mapping closed.
func (s *TopicService) Close(ctx context.Context, topic *telegramtopic.Topic) error {
	if err := s.client.CloseTopic(ctx, s.group, topic.ThreadId); err != nil &&
		!isMissingThreadError(err) && !isTopicNotModifiedError(err) {
		return err
	}
	return s.topics.UpdateStatus(ctx, topic.Id, telegramtopic.StatusClosed)
}

// isTopicNotModifiedError matches the no-op rejection Telegram returns when
// a topic is already in the requested state.
func isTopicNotModifiedError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "topic_not_modified")
}

// ErrRateLimited reports that Telegram throttled the bot (HTTP 429): the
// delivery was not made and nothing about the topic is wrong. Callers that
// can wait retry after the pause the error's RetryAfter names; the others
// report it as a delivery failure.
var ErrRateLimited = errors.New("telegram rate limit")

// RateLimitedError is ErrRateLimited with the pause Telegram asked for.
type RateLimitedError struct {
	RetryAfter time.Duration
	cause      error
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("telegram rate limit, retry after %s: %v", e.RetryAfter, e.cause)
}

func (e *RateLimitedError) Unwrap() []error { return []error{ErrRateLimited, e.cause} }

// rateLimited classifies Telegram's 429 rejection, which the bot library
// reports as a TooManyRequestsError carrying retry_after in seconds.
func rateLimited(err error) (*RateLimitedError, bool) {
	var tooMany *tgbot.TooManyRequestsError
	if !errors.As(err, &tooMany) {
		return nil, false
	}
	retryAfter := time.Duration(tooMany.RetryAfter) * time.Second
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	return &RateLimitedError{RetryAfter: retryAfter, cause: err}, true
}

// relayRetryAfterLimit bounds the pause Relay waits out itself before
// retrying a throttled delivery once. A longer pause is the caller's to
// schedule: the relays run under short request-bound contexts.
const relayRetryAfterLimit = 5 * time.Second

// PostMarkdown sends MarkdownV2 into a topic through Relay's self-healing.
func (s *TopicService) PostMarkdown(ctx context.Context, m TelegramMessenger, topic *telegramtopic.Topic, text string) (*telegramtopic.Topic, error) {
	return s.Relay(ctx, topic, func(threadID int64) error {
		return m.SendMarkdown(ctx, s.group, threadID, text)
	})
}

// PostText is PostMarkdown for plain text.
func (s *TopicService) PostText(ctx context.Context, m TelegramMessenger, topic *telegramtopic.Topic, text string) (*telegramtopic.Topic, error) {
	return s.Relay(ctx, topic, func(threadID int64) error {
		return m.Send(ctx, s.group, threadID, text)
	})
}

// Relay runs op against the topic's thread, transparently recreating a
// deleted topic or reopening a closed one, then retrying once. It returns
// the mapping actually used, which may have been repointed. A delivery
// Telegram throttled (HTTP 429) is distinct from a broken topic: nothing is
// recreated or reopened; when the pause Telegram asks for is short and the
// context allows it, Relay waits it out and retries once, otherwise it
// reports a RateLimitedError so the caller can back off.
func (s *TopicService) Relay(ctx context.Context, topic *telegramtopic.Topic, op func(threadID int64) error) (*telegramtopic.Topic, error) {
	err := op(topic.ThreadId)
	switch {
	case err == nil:
		return topic, nil
	case isMissingThreadError(err):
		repointed, rerr := s.Recreate(ctx, topic)
		if rerr != nil {
			return topic, err
		}
		return repointed, op(repointed.ThreadId)
	case isTopicClosedError(err):
		reopened, rerr := s.Reopen(ctx, topic)
		if rerr != nil {
			return topic, err
		}
		return reopened, op(reopened.ThreadId)
	}
	if limited, ok := rateLimited(err); ok {
		return topic, s.retryAfter(ctx, topic, op, limited)
	}
	return topic, err
}

// retryAfter waits out a throttled delivery's pause and retries it once; a
// pause the context or the bound rules out is reported as it is.
func (s *TopicService) retryAfter(ctx context.Context, topic *telegramtopic.Topic, op func(threadID int64) error, limited *RateLimitedError) error {
	if limited.RetryAfter > relayRetryAfterLimit {
		return limited
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < limited.RetryAfter {
		return limited
	}
	timer := time.NewTimer(limited.RetryAfter)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return limited
	}
	err := op(topic.ThreadId)
	if err == nil {
		return nil
	}
	if again, ok := rateLimited(err); ok {
		return again
	}
	return err
}
