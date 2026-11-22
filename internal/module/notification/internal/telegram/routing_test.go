package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
)

// ───────────────────────── harness ─────────────────────────

const testGroupID int64 = -1001234

type routingHarness struct {
	bot       *Bot
	admin     *fakeAdminHandler
	messenger *recordingMessenger
	topics    *fakeTopicRepo
	client    *fakeTopicClient
	accounts  *fakeAccounts
	tickets   *fakeTickets
}

func newRoutingHarness() *routingHarness {
	adminFlag := true
	notAdmin := false
	enabled := true
	h := &routingHarness{
		admin:     &fakeAdminHandler{},
		messenger: &recordingMessenger{},
		topics:    &fakeTopicRepo{},
		client:    &fakeTopicClient{deadThreads: map[int64]bool{}, closedThreads: map[int64]bool{}},
		tickets:   newFakeTickets(&ticket.Ticket{Id: 321, Status: ticket.Pending}),
		accounts:  newFakeAccounts(),
	}
	// chat 1001 is user 7 (a bound customer); sender 500 is user 9 (a bound
	// administrator)
	h.accounts.addBinding(7, "telegram", "1001")
	h.accounts.addBinding(7, "email", "buyer@example.com")
	h.accounts.addBinding(9, "telegram", "500")
	h.accounts.users[9] = &user.User{Id: 9, IsAdmin: &adminFlag, Enable: &enabled}
	h.accounts.users[7] = &user.User{Id: 7, IsAdmin: &notAdmin, Enable: &enabled}
	h.rebuild(stubLimiter{allow: true})
	return h
}

func (h *routingHarness) rebuild(limiter TelegramRelayLimiter) {
	h.bot = NewBot(BotDependencies{
		Messenger:   h.messenger,
		Accounts:    h.accounts,
		Admin:       h.admin,
		GroupChatID: func() int64 { return testGroupID },
		Topics:      h.topics,
		TopicClient: h.client,
		Tickets:     h.tickets,
		Limiter:     limiter,
	})
}

func privateMessage(chatID int64, text string) *models.Message {
	return &models.Message{
		ID:   42,
		Chat: models.Chat{ID: chatID, Type: models.ChatTypePrivate},
		From: &models.User{ID: chatID, Username: "buyer"},
		Text: text,
	}
}

func groupMessage(sender int64, thread int, text string) *models.Message {
	return &models.Message{
		ID:              99,
		Chat:            models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
		From:            &models.User{ID: sender},
		MessageThreadID: thread,
		Text:            text,
	}
}

func withCommand(msg *models.Message) *models.Message {
	command := msg.Text
	if idx := strings.Index(command, " "); idx != -1 {
		command = command[:idx]
	}
	msg.Entities = []models.MessageEntity{{
		Type:   models.MessageEntityTypeBotCommand,
		Offset: 0,
		Length: len(command),
	}}
	return msg
}

func (h *routingHarness) dispatch(msg *models.Message) {
	h.bot.HandleUpdate(context.Background(), &models.Update{Message: msg})
}

// ───────────────────────── routing ─────────────────────────

// Administrator commands typed in a private chat are redirected, never run.
func TestAdminCommandInPrivateChatIsRedirected(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(withCommand(privateMessage(500, "/dash")))

	if len(h.admin.handled) != 0 {
		t.Fatal("administrator command ran outside the admin group")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "管理群") {
		t.Fatalf("sent = %+v, want a redirect notice", h.messenger.sent)
	}
}

func TestAdminCommandInGroupIsDispatched(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(withCommand(groupMessage(500, 0, "/dash")))

	if len(h.admin.handled) != 1 {
		t.Fatalf("admin handled = %d, want 1", len(h.admin.handled))
	}
}

// A message in any group other than the configured one must be ignored: the
// bot may be a member of other groups, but it serves exactly one.
func TestForeignGroupMessagesAreIgnored(t *testing.T) {
	h := newRoutingHarness()
	msg := withCommand(groupMessage(500, 0, "/dash"))
	msg.Chat.ID = testGroupID - 1
	h.dispatch(msg)

	if len(h.admin.handled) != 0 || len(h.messenger.sent) != 0 {
		t.Fatal("a foreign group's message was processed")
	}
}

// ───────────────────────── support relay ─────────────────────────

func TestSupportRelayRequiresBinding(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(privateMessage(2002, "help me")) // chat 2002 has no binding

	if len(h.client.forwards) != 0 {
		t.Fatal("an unbound user's message was forwarded")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "绑定") {
		t.Fatalf("sent = %+v, want a bind prompt", h.messenger.sent)
	}
}

func TestSupportRelayForwardsIntoUserTopic(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(privateMessage(1001, "订阅无法使用"))

	if len(h.client.createdNames) != 1 {
		t.Fatalf("topics created = %d, want 1", len(h.client.createdNames))
	}
	if name := h.client.createdNames[0]; !strings.Contains(name, "buyer@example.com") || !strings.Contains(name, "@buyer") {
		t.Fatalf("topic name = %q, want the panel account and the username", name)
	}
	if len(h.client.forwards) != 1 {
		t.Fatalf("forwards = %d, want 1", len(h.client.forwards))
	}
	fwd := h.client.forwards[0]
	if fwd.chatID != testGroupID || fwd.fromChatID != 1001 || fwd.threadID != 1 || fwd.messageID != 42 {
		t.Fatalf("forward = %+v, want the user message into thread 1", fwd)
	}
	// The first contact also greets the user.
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "客服") {
		t.Fatalf("sent = %+v, want the first-contact greeting", h.messenger.sent)
	}
}

func TestSupportRelayReusesExistingTopic(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(privateMessage(1001, "第一条"))
	h.dispatch(privateMessage(1001, "第二条"))

	if len(h.client.createdNames) != 1 {
		t.Fatalf("topics created = %d, want 1 (reused)", len(h.client.createdNames))
	}
	if len(h.client.forwards) != 2 {
		t.Fatalf("forwards = %d, want 2", len(h.client.forwards))
	}
	if len(h.messenger.sent) != 1 {
		t.Fatalf("greetings = %d, want the greeting only once", len(h.messenger.sent))
	}
}

func TestSupportRelayRateLimitRejects(t *testing.T) {
	h := newRoutingHarness()
	h.rebuild(stubLimiter{allow: false, notify: true})
	h.dispatch(privateMessage(1001, "flood"))

	if len(h.client.forwards) != 0 {
		t.Fatal("a rate-limited message was forwarded")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "频繁") {
		t.Fatalf("sent = %+v, want a rate-limit notice", h.messenger.sent)
	}
}

// Beyond the first rejection of a window the bot stays silent, otherwise a
// flood of incoming messages becomes a flood of outgoing notices.
func TestSupportRelayRateLimitNotifiesOnlyOnce(t *testing.T) {
	h := newRoutingHarness()
	h.rebuild(stubLimiter{allow: false, notify: false})
	h.dispatch(privateMessage(1001, "flood"))

	if len(h.client.forwards) != 0 || len(h.messenger.sent) != 0 {
		t.Fatalf("forwards = %d, sent = %+v, want complete silence", len(h.client.forwards), h.messenger.sent)
	}
}

// A topic deleted inside Telegram heals: the mapping is repointed to a new
// topic and the message still arrives.
func TestSupportRelayRecreatesDeletedTopic(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(privateMessage(1001, "第一条")) // creates thread 1
	h.client.deadThreads[1] = true
	h.dispatch(privateMessage(1001, "第二条"))

	if len(h.client.createdNames) != 2 {
		t.Fatalf("topics created = %d, want the dead topic recreated", len(h.client.createdNames))
	}
	last := h.client.forwards[len(h.client.forwards)-1]
	if last.threadID != 2 {
		t.Fatalf("forward thread = %d, want the recreated thread 2", last.threadID)
	}
	if mapped, err := h.topics.FindByKindRef(context.Background(), testGroupID, telegramtopic.KindSupport, 7); err != nil || mapped.ThreadId != 2 {
		t.Fatalf("mapping = %+v (err %v), want repointed to thread 2", mapped, err)
	}
}

// ───────────────────────── group-side relay ─────────────────────────

func (h *routingHarness) seedTopic(kind uint8, refID, threadID int64, status uint8) {
	_ = h.topics.Insert(context.Background(), &telegramtopic.Topic{
		ChatId: testGroupID, Kind: kind, RefId: refID, ThreadId: threadID, Status: status,
	})
}

func TestAdminReplyInSupportTopicIsCopiedToUser(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
	h.dispatch(groupMessage(500, 11, "您好，已处理"))

	if len(h.client.copies) != 1 {
		t.Fatalf("copies = %d, want 1", len(h.client.copies))
	}
	got := h.client.copies[0]
	if got.toChatID != 1001 || got.fromChatID != testGroupID || got.messageID != 99 {
		t.Fatalf("copy = %+v, want the group message copied to chat 1001", got)
	}
}

func TestNonAdminReplyInSupportTopicIsRejectedAloud(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
	h.dispatch(groupMessage(666, 11, "我也来说两句")) // 666 is not bound

	if len(h.client.copies) != 0 {
		t.Fatal("a non-administrator's message reached the user")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "未送达") {
		t.Fatalf("sent = %+v, want an explicit rejection in the topic", h.messenger.sent)
	}
	if h.messenger.sent[0].threadID != 11 {
		t.Fatalf("rejection thread = %d, want the same topic", h.messenger.sent[0].threadID)
	}
}

func TestAdminReplyInTicketTopicBecomesFollow(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusActive)
	h.dispatch(groupMessage(500, 12, "请重启客户端再试"))

	// The reply runs the support use case like /rp, but the topic already
	// shows it, so it is not mirrored back.
	want := ticketReply{id: 321, from: staffAuthor, content: "请重启客户端再试", inTopic: true}
	if len(h.tickets.replies) != 1 || h.tickets.replies[0] != want {
		t.Fatalf("replies = %+v, want %+v", h.tickets.replies, want)
	}
	if got := h.tickets.tickets[321].Status; got != ticket.Waiting {
		t.Fatalf("status = %d, want ticket 321 flipped to Waiting", got)
	}
}

// A bound user who is not an administrator must also be rejected aloud —
// this is the common case, distinct from the unbound stranger above.
func TestBoundNonAdminReplyInSupportTopicIsRejected(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
	h.dispatch(groupMessage(1001, 11, "插一句")) // sender 1001 is bound to user 7, not an admin

	if len(h.client.copies) != 0 {
		t.Fatal("a non-administrator's message reached the user")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "不是管理员") {
		t.Fatalf("sent = %+v, want the non-admin rejection", h.messenger.sent)
	}
}

// Chatter inside the notification feed topic is nobody's conversation: no
// relay, no rejection noise.
func TestChatterInNotifyTopicIsIgnored(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindNotify, 0, 3, telegramtopic.StatusActive)
	h.dispatch(groupMessage(666, 3, "路过"))

	if len(h.messenger.sent) != 0 || len(h.client.copies) != 0 {
		t.Fatalf("sent = %+v, want silence in the notify topic", h.messenger.sent)
	}
}

func TestClosingTicketTopicClosesTicket(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusActive)
	h.dispatch(topicClosedBy(500, 12)) // 500 is the bound administrator

	if len(h.tickets.statuses) != 1 || h.tickets.statuses[0] != (ticketStatusChange{id: 321, status: ticket.Closed, inTopic: true}) {
		t.Fatalf("statuses = %+v, want ticket 321 closed without a mirror back into the topic", h.tickets.statuses)
	}
	mapped, err := h.topics.FindByThread(context.Background(), testGroupID, 12)
	if err != nil || mapped.Status != telegramtopic.StatusClosed {
		t.Fatalf("mapping = %+v (err %v), want closed", mapped, err)
	}
}

func topicClosedBy(sender int64, thread int) *models.Message {
	return &models.Message{
		Chat:             models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
		From:             &models.User{ID: sender},
		MessageThreadID:  thread,
		ForumTopicClosed: &models.ForumTopicClosed{},
	}
}

func topicReopenedBy(sender int64, thread int) *models.Message {
	return &models.Message{
		Chat:               models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
		From:               &models.User{ID: sender},
		MessageThreadID:    thread,
		ForumTopicReopened: &models.ForumTopicReopened{},
	}
}

// Any member with the manage-topics right can close or reopen a topic in
// Telegram; only a panel administrator's action may change the ticket. The
// mapping still follows the topic's real state, and the refusal is spoken.
func TestNonAdminTopicLifecycleDoesNotChangeTicket(t *testing.T) {
	for name, msg := range map[string]*models.Message{
		"bound non-admin closes":  topicClosedBy(1001, 12),
		"unbound member closes":   topicClosedBy(666, 12),
		"bound non-admin reopens": topicReopenedBy(1001, 12),
		"sender unknown closes": {
			Chat:             models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
			MessageThreadID:  12,
			ForumTopicClosed: &models.ForumTopicClosed{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newRoutingHarness()
			h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusActive)
			h.dispatch(msg)

			if len(h.tickets.statuses) != 0 {
				t.Fatalf("statuses = %+v, want the ticket untouched", h.tickets.statuses)
			}
			if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "工单状态未同步") || h.messenger.sent[0].threadID != 12 {
				t.Fatalf("sent = %+v, want a spoken refusal in the topic", h.messenger.sent)
			}
			wantStatus := uint8(telegramtopic.StatusClosed)
			if msg.ForumTopicReopened != nil {
				wantStatus = telegramtopic.StatusActive
			}
			if mapped, err := h.topics.FindByThread(context.Background(), testGroupID, 12); err != nil || mapped.Status != wantStatus {
				t.Fatalf("mapping = %+v (err %v), want status %d mirroring Telegram", mapped, err, wantStatus)
			}
		})
	}
}

// FindOne is unscoped, so a deleted or disabled administrator still comes
// back from the lookup; neither may act through the group any more.
func TestInactiveAdminLosesGroupAuthority(t *testing.T) {
	for name, deactivate := range map[string]func(*user.User){
		"deleted":  func(u *user.User) { u.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true} },
		"disabled": func(u *user.User) { u.Enable = new(bool) },
	} {
		t.Run(name, func(t *testing.T) {
			h := newRoutingHarness()
			deactivate(h.accounts.users[9])
			h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
			h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusActive)

			h.dispatch(groupMessage(500, 11, "support reply"))
			h.dispatch(groupMessage(500, 12, "ticket reply"))
			h.dispatch(topicClosedBy(500, 12))

			if len(h.client.copies) != 0 || len(h.tickets.replies) != 0 || len(h.tickets.statuses) != 0 {
				t.Fatalf("copies = %+v, replies = %+v, statuses = %+v, want no effect", h.client.copies, h.tickets.replies, h.tickets.statuses)
			}
			if len(h.messenger.sent) != 3 {
				t.Fatalf("sent = %+v, want one spoken refusal per action", h.messenger.sent)
			}
			for _, sent := range h.messenger.sent {
				if !strings.Contains(sent.message, "不是管理员") {
					t.Fatalf("sent = %+v, want the non-admin refusal", h.messenger.sent)
				}
			}
		})
	}
}

// Closing a live-chat topic tells the customer the conversation ended, so
// only an administrator's close may send that notice.
func TestClosingSupportTopicNotifiesCustomerOnlyForAdmins(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
	h.dispatch(topicClosedBy(1001, 11)) // bound non-admin
	for _, sent := range h.messenger.sent {
		if sent.chatID == 1001 {
			t.Fatalf("sent = %+v, a non-admin's close reached the customer", h.messenger.sent)
		}
	}

	h = newRoutingHarness()
	h.seedTopic(telegramtopic.KindSupport, 7, 11, telegramtopic.StatusActive)
	h.dispatch(topicClosedBy(500, 11)) // administrator
	if len(h.messenger.sent) != 1 || h.messenger.sent[0].chatID != 1001 || !strings.Contains(h.messenger.sent[0].message, "会话已结束") {
		t.Fatalf("sent = %+v, want the end-of-conversation notice to the customer", h.messenger.sent)
	}
}

// The bot's own Close/Reopen calls emit the same service messages a human
// produces. Syncing them back would overwrite statuses the website side
// just wrote (Waiting → Pending), so they must be ignored.
func TestBotReopenServiceMessageDoesNotRewriteTicketStatus(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusClosed)
	h.dispatch(&models.Message{
		Chat:               models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
		From:               &models.User{ID: 424242, IsBot: true},
		MessageThreadID:    12,
		ForumTopicReopened: &models.ForumTopicReopened{},
	})

	if len(h.tickets.statuses) != 0 {
		t.Fatalf("statuses = %+v, want the bot's own service message ignored", h.tickets.statuses)
	}
}

// A human reopening the topic in the Telegram UI genuinely reopens the
// ticket.
func TestHumanReopeningTicketTopicReopensTicket(t *testing.T) {
	h := newRoutingHarness()
	h.seedTopic(telegramtopic.KindTicket, 321, 12, telegramtopic.StatusClosed)
	h.dispatch(&models.Message{
		Chat:               models.Chat{ID: testGroupID, Type: models.ChatTypeSupergroup},
		From:               &models.User{ID: 500},
		MessageThreadID:    12,
		ForumTopicReopened: &models.ForumTopicReopened{},
	})

	if len(h.tickets.statuses) != 1 || h.tickets.statuses[0] != (ticketStatusChange{id: 321, status: ticket.Pending, inTopic: true}) {
		t.Fatalf("statuses = %+v, want ticket 321 reopened to Pending", h.tickets.statuses)
	}
	mapped, err := h.topics.FindByThread(context.Background(), testGroupID, 12)
	if err != nil || mapped.Status != telegramtopic.StatusActive {
		t.Fatalf("mapping = %+v (err %v), want active", mapped, err)
	}
}

// ───────────────────────── notify topic ─────────────────────────

// Ensure must adopt the winner's row when two callers race on first use.
func TestEnsureAdoptsExistingMappingOnDuplicate(t *testing.T) {
	repo := &fakeTopicRepo{}
	client := &fakeTopicClient{deadThreads: map[int64]bool{}}
	svc := NewTopicService(client, repo, testGroupID)

	first, created, err := svc.Ensure(context.Background(), telegramtopic.KindNotify, 0, NotifyTopicTitle)
	if err != nil || !created {
		t.Fatalf("first ensure = (%+v, %v, %v), want a created topic", first, created, err)
	}
	second, created, err := svc.Ensure(context.Background(), telegramtopic.KindNotify, 0, NotifyTopicTitle)
	if err != nil || created {
		t.Fatalf("second ensure = (%v, %v), want the same mapping without a create", created, err)
	}
	if second.ThreadId != first.ThreadId {
		t.Fatalf("thread = %d, want %d", second.ThreadId, first.ThreadId)
	}
	if len(client.createdNames) != 1 {
		t.Fatalf("topics created = %d, want 1", len(client.createdNames))
	}
}

// racingTopicRepo makes the first FindByKindRef miss, simulating two
// concurrent creators: this caller loses the Insert race and must adopt the
// pre-seeded winner's row.
type racingTopicRepo struct {
	*fakeTopicRepo
	missedOnce bool
}

var _ repository.TelegramTopicRepo = (*racingTopicRepo)(nil)

func (r *racingTopicRepo) FindByKindRef(ctx context.Context, chatID int64, kind uint8, refID int64) (*telegramtopic.Topic, error) {
	if !r.missedOnce {
		r.missedOnce = true
		return nil, gorm.ErrRecordNotFound
	}
	return r.fakeTopicRepo.FindByKindRef(ctx, chatID, kind, refID)
}

// The unique-key loser adopts the winner's mapping and deletes the topic it
// created, so staff can never reply into an unmapped orphan.
func TestEnsureInsertConflictAdoptsWinnerAndDeletesOrphan(t *testing.T) {
	inner := &fakeTopicRepo{}
	winner := &telegramtopic.Topic{
		ChatId: testGroupID, Kind: telegramtopic.KindSupport, RefId: 7,
		ThreadId: 77, Status: telegramtopic.StatusActive,
	}
	if err := inner.Insert(context.Background(), winner); err != nil {
		t.Fatalf("seed winner: %v", err)
	}
	client := &fakeTopicClient{nextThread: 100, deadThreads: map[int64]bool{}, closedThreads: map[int64]bool{}}
	svc := NewTopicService(client, &racingTopicRepo{fakeTopicRepo: inner}, testGroupID)

	adopted, created, err := svc.Ensure(context.Background(), telegramtopic.KindSupport, 7, "💬 loser")
	if err != nil {
		t.Fatalf("ensure error = %v", err)
	}
	if created || adopted.ThreadId != 77 {
		t.Fatalf("adopted = (%+v, created=%v), want the winner's thread 77", adopted, created)
	}
	if len(client.createdNames) != 1 {
		t.Fatalf("topics created = %d, want the loser's single create", len(client.createdNames))
	}
	if len(client.deleted) != 1 || client.deleted[0] != 101 {
		t.Fatalf("deleted = %v, want the orphaned thread 101 removed", client.deleted)
	}
}

// A closed topic self-heals on delivery: reopen once, then retry.
func TestRelayReopensClosedTopicAndRetries(t *testing.T) {
	repo := &fakeTopicRepo{}
	seeded := &telegramtopic.Topic{
		ChatId: testGroupID, Kind: telegramtopic.KindSupport, RefId: 7,
		ThreadId: 5, Status: telegramtopic.StatusClosed,
	}
	if err := repo.Insert(context.Background(), seeded); err != nil {
		t.Fatalf("seed: %v", err)
	}
	client := &fakeTopicClient{deadThreads: map[int64]bool{}, closedThreads: map[int64]bool{5: true}}
	svc := NewTopicService(client, repo, testGroupID)

	calls := 0
	relayed, err := svc.Relay(context.Background(), seeded, func(threadID int64) error {
		calls++
		if client.closedThreads[threadID] {
			return errors.New("Bad Request: TOPIC_CLOSED")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("relay error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("op calls = %d, want reopen-then-retry", calls)
	}
	if len(client.reopened) != 1 || client.reopened[0] != 5 {
		t.Fatalf("reopened = %v, want thread 5", client.reopened)
	}
	if relayed.Status != telegramtopic.StatusActive {
		t.Fatalf("status = %d, want active after reopen", relayed.Status)
	}
}

// /help sits in the public menu, so the private chat must answer it with
// user-facing help instead of the admin redirect.
func TestPrivateHelpShowsUserHelp(t *testing.T) {
	h := newRoutingHarness()
	h.dispatch(withCommand(privateMessage(1001, "/help")))

	if len(h.admin.handled) != 0 {
		t.Fatal("/help in private ran as an administrator command")
	}
	if len(h.messenger.sent) != 1 || !strings.Contains(h.messenger.sent[0].message, "绑定") {
		t.Fatalf("sent = %+v, want the user help text", h.messenger.sent)
	}
}

func TestClampTopicTitle(t *testing.T) {
	long := strings.Repeat("标", 200)
	clamped := clampTopicTitle(long)
	if got := len([]rune(clamped)); got != topicTitleLimit {
		t.Fatalf("clamped length = %d, want %d", got, topicTitleLimit)
	}
	if short := clampTopicTitle("💬 short"); short != "💬 short" {
		t.Fatalf("short title changed: %q", short)
	}
}
