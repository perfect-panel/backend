package telegram

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Every fake implements its whole port, so a port that grows fails to
// compile here instead of panicking in a test.
var (
	_ TelegramMessenger            = (*recordingMessenger)(nil)
	_ TelegramAdminHandler         = (*fakeAdminHandler)(nil)
	_ TelegramSessionStore         = (*fakeRedisStore)(nil)
	_ TelegramAdminActionStore     = (*fakeRedisStore)(nil)
	_ TelegramRelayLimiter         = stubLimiter{}
	_ Accounts                     = (*fakeAccounts)(nil)
	_ Tickets                      = (*fakeTickets)(nil)
	_ Subscriptions                = (*fakeSubscriptions)(nil)
	_ Billing                      = fakeBilling{}
	_ AuditLogs                    = (*fakeAuditLogs)(nil)
	_ repository.TelegramTopicRepo = (*fakeTopicRepo)(nil)
	_ TelegramTopicClient          = (*fakeTopicClient)(nil)
)

// ───────────────────────── messaging ─────────────────────────

type sentTelegramMessage struct {
	chatID   int64
	threadID int64
	message  string
	markdown bool
}

type recordingMessenger struct {
	sent []sentTelegramMessage
}

func (m *recordingMessenger) Send(_ context.Context, chatID, threadID int64, message string) error {
	m.sent = append(m.sent, sentTelegramMessage{chatID: chatID, threadID: threadID, message: message})
	return nil
}

func (m *recordingMessenger) SendMarkdown(_ context.Context, chatID, threadID int64, message string) error {
	m.sent = append(m.sent, sentTelegramMessage{chatID: chatID, threadID: threadID, message: message, markdown: true})
	return nil
}

// last returns the most recent message, or the zero message when none went
// out.
func (m *recordingMessenger) last() sentTelegramMessage {
	if len(m.sent) == 0 {
		return sentTelegramMessage{}
	}
	return m.sent[len(m.sent)-1]
}

type fakeAdminHandler struct {
	handled []*models.Message
}

func (h *fakeAdminHandler) Handle(_ context.Context, msg *models.Message) {
	h.handled = append(h.handled, msg)
}

// fakeTokenTTL is the life a seeded binding token has left.
const fakeTokenTTL = 4 * time.Minute

// fakeRedisStore stands in for binding tokens, binding locks and
// administrator confirmations; a missing key reads as redis.Nil, as Redis
// reports it.
type fakeRedisStore struct {
	values map[string]string
	// ttls records the life of every key Set stored; a seeded key has
	// fakeTokenTTL left.
	ttls map[string]time.Duration
	// deleted records the keys removed through Delete; a GetDel or Take
	// consumes its key without a trace, like the Redis command.
	deleted []string
	// err fails every call when set, like Redis being unreachable.
	err error
}

func (s *fakeRedisStore) Get(_ context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	value, ok := s.values[key]
	if !ok {
		return "", redis.Nil
	}
	return value, nil
}

func (s *fakeRedisStore) GetDel(ctx context.Context, key string) (string, error) {
	value, _, err := s.Take(ctx, key)
	return value, err
}

func (s *fakeRedisStore) Take(ctx context.Context, key string) (string, time.Duration, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return "", 0, err
	}
	ttl := fakeTokenTTL
	if stored, ok := s.ttls[key]; ok {
		ttl = stored
	}
	delete(s.values, key)
	delete(s.ttls, key)
	return value, ttl, nil
}

func (s *fakeRedisStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if s.err != nil {
		return s.err
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	if s.ttls == nil {
		s.ttls = make(map[string]time.Duration)
	}
	s.values[key] = value
	s.ttls[key] = ttl
	return nil
}

func (s *fakeRedisStore) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if _, held := s.values[key]; held {
		return false, nil
	}
	return true, s.Set(ctx, key, "1", ttl)
}

func (s *fakeRedisStore) Delete(_ context.Context, key string) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = append(s.deleted, key)
	delete(s.values, key)
	delete(s.ttls, key)
	return nil
}

type stubLimiter struct{ allow, notify bool }

func (l stubLimiter) Allow(context.Context, int64) (bool, bool) { return l.allow, l.notify }

// ───────────────────────── identity ─────────────────────────

type fakeAccounts struct {
	users map[int64]*user.User
	// byIdentifier is keyed by authType:identifier, byUser by
	// authType:userID.
	byIdentifier map[string]*user.AuthMethods
	byUser       map[string]*user.AuthMethods
	bound        []*user.AuthMethods
	registered   int64
	err          error
	// afterSetEnabled, when set, runs once right after the next SetEnabled
	// write, so a test can interleave a second command with the first one's
	// write.
	afterSetEnabled func()
	// beforeBind, when set, runs once right before the next binding is
	// recorded, so a test can interleave a second redemption with the first
	// one's insert.
	beforeBind func()
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{
		users:        map[int64]*user.User{},
		byIdentifier: map[string]*user.AuthMethods{},
		byUser:       map[string]*user.AuthMethods{},
	}
}

// addBinding records an auth method under both lookups.
func (f *fakeAccounts) addBinding(userID int64, authType, identifier string) {
	method := &user.AuthMethods{Id: int64(len(f.byUser) + 1), UserId: userID, AuthType: authType, AuthIdentifier: identifier}
	f.byIdentifier[authType+":"+identifier] = method
	f.byUser[authType+":"+strconv.FormatInt(userID, 10)] = method
}

func (f *fakeAccounts) FindUser(_ context.Context, id int64) (*user.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.users[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *u
	return &copied, nil
}

func (f *fakeAccounts) FindBinding(_ context.Context, authType, identifier string) (*user.AuthMethods, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.byIdentifier[authType+":"+identifier]; ok {
		copied := *m
		return &copied, nil
	}
	return &user.AuthMethods{}, gorm.ErrRecordNotFound
}

func (f *fakeAccounts) FindUserBinding(_ context.Context, userID int64, authType string) (*user.AuthMethods, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.byUser[authType+":"+strconv.FormatInt(userID, 10)]; ok {
		copied := *m
		return &copied, nil
	}
	return &user.AuthMethods{}, gorm.ErrRecordNotFound
}

func (f *fakeAccounts) ListBindings(_ context.Context, userID int64) ([]*user.AuthMethods, error) {
	var list []*user.AuthMethods
	for _, m := range f.byUser {
		if m.UserId == userID {
			copied := *m
			list = append(list, &copied)
		}
	}
	slices.SortFunc(list, func(a, b *user.AuthMethods) int { return int(a.Id - b.Id) })
	return list, nil
}

func (f *fakeAccounts) BindTelegram(_ context.Context, userID int64, chatID string) error {
	if f.err != nil {
		return f.err
	}
	if hook := f.beforeBind; hook != nil {
		f.beforeBind = nil
		hook()
	}
	f.addBinding(userID, "telegram", chatID)
	f.bound = append(f.bound, &user.AuthMethods{UserId: userID, AuthType: "telegram", AuthIdentifier: chatID})
	return nil
}

func (f *fakeAccounts) SetEnabled(_ context.Context, userID int64, enabled bool) error {
	u, ok := f.users[userID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	u.Enable = &enabled
	if hook := f.afterSetEnabled; hook != nil {
		f.afterSetEnabled = nil
		hook()
	}
	return nil
}

func (f *fakeAccounts) CountRegistrations(context.Context, time.Time) (int64, error) {
	return f.registered, nil
}

// ───────────────────────── support ─────────────────────────

type ticketStatusChange struct {
	id      int64
	status  uint8
	inTopic bool
}

type ticketReply struct {
	id            int64
	from, content string
	inTopic       bool
}

// fakeTickets applies replies and status changes to its tickets the way the
// support use case does: a reply moves the ticket to Waiting, and a closed
// ticket refuses a staff reply with ticket.ErrClosed.
type fakeTickets struct {
	tickets  map[int64]*ticket.Ticket
	details  *ticket.Details
	replies  []ticketReply
	statuses []ticketStatusChange
	pending  int64
}

func newFakeTickets(tickets ...*ticket.Ticket) *fakeTickets {
	f := &fakeTickets{tickets: map[int64]*ticket.Ticket{}}
	for _, t := range tickets {
		f.tickets[t.Id] = t
	}
	return f
}

func (f *fakeTickets) CountAwaitingReply(context.Context) (int64, error) { return f.pending, nil }

func (f *fakeTickets) List(_ context.Context, _, size int, status *uint8) (int64, []*ticket.Ticket, error) {
	var list []*ticket.Ticket
	for _, t := range f.tickets {
		if status == nil || t.Status == *status {
			list = append(list, t)
		}
	}
	slices.SortFunc(list, func(a, b *ticket.Ticket) int { return int(a.Id - b.Id) })
	total := int64(len(list))
	if len(list) > size {
		list = list[:size]
	}
	return total, list, nil
}

func (f *fakeTickets) Find(_ context.Context, id int64) (*ticket.Ticket, error) {
	t, ok := f.tickets[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *t
	return &copied, nil
}

func (f *fakeTickets) Detail(context.Context, int64) (*ticket.Details, error) {
	if f.details == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return f.details, nil
}

func (f *fakeTickets) Reply(_ context.Context, id int64, from, content string, inTopic bool) (uint8, error) {
	t, ok := f.tickets[id]
	if !ok {
		return 0, errors.Join(errors.New("find ticket"), gorm.ErrRecordNotFound)
	}
	if t.Status == ticket.Closed {
		return 0, errors.Join(errors.New("staff reply"), ticket.ErrClosed)
	}
	previous := t.Status
	t.Status = ticket.Waiting
	f.replies = append(f.replies, ticketReply{id: id, from: from, content: content, inTopic: inTopic})
	return previous, nil
}

func (f *fakeTickets) SetStatus(_ context.Context, id int64, status uint8, inTopic bool) error {
	t, ok := f.tickets[id]
	if !ok {
		return errors.Join(errors.New("find ticket"), gorm.ErrRecordNotFound)
	}
	t.Status = status
	f.statuses = append(f.statuses, ticketStatusChange{id: id, status: status, inTopic: inTopic})
	return nil
}

// ───────────────────────── subscription ─────────────────────────

type fakeSubscriptions struct {
	subs   map[int64]*usersub.Subscribe
	byUser map[int64][]*usersub.SubscribeDetails
	// writes counts the subscription writes the bot asked for.
	writes int
}

func newFakeSubscriptions(subs ...*usersub.Subscribe) *fakeSubscriptions {
	f := &fakeSubscriptions{subs: map[int64]*usersub.Subscribe{}, byUser: map[int64][]*usersub.SubscribeDetails{}}
	for _, s := range subs {
		f.subs[s.Id] = s
	}
	return f
}

func (f *fakeSubscriptions) Find(_ context.Context, id int64) (*usersub.Subscribe, error) {
	s, ok := f.subs[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *s
	return &copied, nil
}

func (f *fakeSubscriptions) ListByUser(_ context.Context, userID int64) ([]*usersub.SubscribeDetails, error) {
	return f.byUser[userID], nil
}

func (f *fakeSubscriptions) ResetTraffic(_ context.Context, sub *usersub.Subscribe) error {
	f.writes++
	stored := f.subs[sub.Id]
	stored.Download, stored.Upload = 0, 0
	return nil
}

func (f *fakeSubscriptions) SetStatus(_ context.Context, sub *usersub.Subscribe, status uint8) error {
	f.writes++
	f.subs[sub.Id].Status = status
	return nil
}

// ───────────────────────── billing / audit ─────────────────────────

type fakeBilling struct {
	revenue  int64
	balances map[int64]int64
}

func (f fakeBilling) Revenue(context.Context, time.Time) (int64, error) { return f.revenue, nil }

func (f fakeBilling) Balance(_ context.Context, userID int64) (int64, error) {
	return f.balances[userID], nil
}

type fakeAuditLogs struct {
	logins []*log.SystemLog
	// rows records the audit rows the bot wrote.
	rows []*log.SystemLog
}

func (f *fakeAuditLogs) RecentLogins(_ context.Context, _ int64, limit int) ([]*log.SystemLog, error) {
	if len(f.logins) > limit {
		return f.logins[:limit], nil
	}
	return f.logins, nil
}

func (f *fakeAuditLogs) Insert(_ context.Context, row *log.SystemLog) error {
	f.rows = append(f.rows, row)
	return nil
}

// actions decodes the administrator actions recorded so far.
func (f *fakeAuditLogs) actions(t *testing.T) []log.AdminAction {
	t.Helper()
	actions := make([]log.AdminAction, 0, len(f.rows))
	for _, row := range f.rows {
		if row.Type != log.TypeAdminAction.Uint8() {
			t.Fatalf("row %+v is not an administrator action", row)
		}
		var action log.AdminAction
		if err := action.Unmarshal([]byte(row.Content)); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	return actions
}

// ───────────────────────── topics ─────────────────────────

type fakeTopicRepo struct {
	rows   []*telegramtopic.Topic
	nextID int64
}

func (r *fakeTopicRepo) Insert(_ context.Context, data *telegramtopic.Topic) error {
	for _, row := range r.rows {
		if row.ChatId == data.ChatId && row.Kind == data.Kind && row.RefId == data.RefId {
			return errors.New("duplicate kind/ref")
		}
		if row.ChatId == data.ChatId && row.ThreadId == data.ThreadId {
			return errors.New("duplicate thread")
		}
	}
	r.nextID++
	data.Id = r.nextID
	copied := *data
	r.rows = append(r.rows, &copied)
	return nil
}

func (r *fakeTopicRepo) FindByKindRef(_ context.Context, chatID int64, kind uint8, refID int64) (*telegramtopic.Topic, error) {
	for _, row := range r.rows {
		if row.ChatId == chatID && row.Kind == kind && row.RefId == refID {
			copied := *row
			return &copied, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeTopicRepo) FindByThread(_ context.Context, chatID, threadID int64) (*telegramtopic.Topic, error) {
	for _, row := range r.rows {
		if row.ChatId == chatID && row.ThreadId == threadID {
			copied := *row
			return &copied, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeTopicRepo) UpdateThread(_ context.Context, id, threadID int64) error {
	for _, row := range r.rows {
		if row.Id == id {
			row.ThreadId = threadID
			row.Status = telegramtopic.StatusActive
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *fakeTopicRepo) UpdateStatus(_ context.Context, id int64, status uint8) error {
	for _, row := range r.rows {
		if row.Id == id {
			row.Status = status
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

type forwardedMessage struct {
	chatID, threadID, fromChatID int64
	messageID                    int
}

type copiedMessage struct {
	toChatID, fromChatID int64
	messageID            int
}

type fakeTopicClient struct {
	nextThread   int64
	createdNames []string
	forwards     []forwardedMessage
	copies       []copiedMessage
	closed       []int64
	reopened     []int64
	deleted      []int64
	// deadThreads simulates topics deleted inside Telegram; closedThreads
	// simulates topics closed inside Telegram.
	deadThreads   map[int64]bool
	closedThreads map[int64]bool
}

func (c *fakeTopicClient) ValidateAdminGroup(context.Context, int64) error { return nil }

func (c *fakeTopicClient) CreateTopic(_ context.Context, _ int64, name string) (int64, error) {
	c.nextThread++
	c.createdNames = append(c.createdNames, name)
	return c.nextThread, nil
}

func (c *fakeTopicClient) DeleteTopic(_ context.Context, _ int64, threadID int64) error {
	c.deleted = append(c.deleted, threadID)
	return nil
}

func (c *fakeTopicClient) CloseTopic(_ context.Context, _ int64, threadID int64) error {
	c.closed = append(c.closed, threadID)
	return nil
}

func (c *fakeTopicClient) ReopenTopic(_ context.Context, _ int64, threadID int64) error {
	c.reopened = append(c.reopened, threadID)
	delete(c.closedThreads, threadID)
	return nil
}

func (c *fakeTopicClient) ForwardToThread(_ context.Context, chatID, threadID, fromChatID int64, messageID int) error {
	if c.deadThreads[threadID] {
		return errors.New("Bad Request: message thread not found")
	}
	if c.closedThreads[threadID] {
		return errors.New("Bad Request: TOPIC_CLOSED")
	}
	c.forwards = append(c.forwards, forwardedMessage{chatID, threadID, fromChatID, messageID})
	return nil
}

func (c *fakeTopicClient) CopyTo(_ context.Context, toChatID, fromChatID int64, messageID int) error {
	c.copies = append(c.copies, copiedMessage{toChatID, fromChatID, messageID})
	return nil
}

// ───────────────────────── messages ─────────────────────────

// telegramCommand builds a command message the way a private chat produces
// it: the sender's user id equals the chat id. Group-context tests override
// Chat/From/MessageThreadID on the result.
func telegramCommand(chatID int64, command string) *models.Message {
	name := command
	for i, r := range command {
		if r == ' ' {
			name = command[:i]
			break
		}
	}
	return &models.Message{
		Chat: models.Chat{ID: chatID, Type: models.ChatTypePrivate},
		From: &models.User{ID: chatID},
		Text: command,
		Entities: []models.MessageEntity{{
			Type:   models.MessageEntityTypeBotCommand,
			Offset: 0,
			Length: len(name),
		}},
	}
}
