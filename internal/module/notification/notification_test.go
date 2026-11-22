package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	tgbot "github.com/go-telegram/bot"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/notification/entity/telegramtopic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// telegramAPI is a fake Bot API server: it records every call and answers
// the methods the facade uses.
type telegramAPI struct {
	mu         sync.Mutex
	calls      []apiCall
	nextThread int
	// chat and member are the getChat and getChatMember answers.
	chat, member string
}

type apiCall struct {
	method string
	form   map[string]string
}

func (a *telegramAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil && r.ContentLength > 0 {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	form := map[string]string{}
	if r.MultipartForm != nil {
		for key, values := range r.MultipartForm.Value {
			form[key] = values[0]
		}
	}
	a.mu.Lock()
	a.calls = append(a.calls, apiCall{method: method, form: form})
	var result string
	switch method {
	case "sendMessage", "copyMessage", "forwardMessage":
		result = `{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}`
	case "createForumTopic":
		a.nextThread++
		result = fmt.Sprintf(`{"message_thread_id":%d,"name":%q,"icon_color":0}`, a.nextThread, form["name"])
	case "getChat":
		result = a.chat
	case "getChatMember":
		result = a.member
	default:
		result = "true"
	}
	a.mu.Unlock()
	_, _ = fmt.Fprintf(w, `{"ok":true,"result":%s}`, result)
}

func (a *telegramAPI) called(method string) []map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var forms []map[string]string
	for _, call := range a.calls {
		if call.method == method {
			forms = append(forms, call.form)
		}
	}
	return forms
}

func (a *telegramAPI) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

// errNotInFacadeTests answers the writes no facade test makes.
var errNotInFacadeTests = errors.New("not used by the facade tests")

var (
	_ notification.Accounts      = fakeAccounts{}
	_ notification.Subscriptions = fakeSubscriptions{}
	_ notification.Tickets       = noTickets{}
	_ notification.Billing       = noBilling{}
	_ notification.AuditLogs     = noLogs{}
)

// fakeAccounts knows the bindings it is given and no account.
type fakeAccounts struct {
	// byUser is keyed by authType:userID, byIdentifier by
	// authType:identifier.
	byUser       map[string]*user.AuthMethods
	byIdentifier map[string]*user.AuthMethods
}

func (f fakeAccounts) FindUser(context.Context, int64) (*user.User, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f fakeAccounts) FindUserBinding(_ context.Context, userID int64, authType string) (*user.AuthMethods, error) {
	if m, ok := f.byUser[authType+":"+strconv.FormatInt(userID, 10)]; ok {
		return m, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f fakeAccounts) FindBinding(_ context.Context, authType, identifier string) (*user.AuthMethods, error) {
	if m, ok := f.byIdentifier[authType+":"+identifier]; ok {
		return m, nil
	}
	return &user.AuthMethods{}, gorm.ErrRecordNotFound
}

func (f fakeAccounts) ListBindings(_ context.Context, userID int64) ([]*user.AuthMethods, error) {
	var list []*user.AuthMethods
	for _, m := range f.byUser {
		if m.UserId == userID {
			list = append(list, m)
		}
	}
	slices.SortFunc(list, func(a, b *user.AuthMethods) int { return int(a.Id - b.Id) })
	return list, nil
}

func (f fakeAccounts) BindTelegram(context.Context, int64, string) error { return errNotInFacadeTests }

func (f fakeAccounts) SetEnabled(context.Context, int64, bool) error { return errNotInFacadeTests }

func (f fakeAccounts) CountRegistrations(context.Context, time.Time) (int64, error) { return 0, nil }

// fakeSubscriptions lists the subscriptions it is given for every user.
type fakeSubscriptions struct {
	subs []*usersub.SubscribeDetails
}

func (f fakeSubscriptions) Find(context.Context, int64) (*usersub.Subscribe, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f fakeSubscriptions) ListByUser(context.Context, int64) ([]*usersub.SubscribeDetails, error) {
	return f.subs, nil
}

func (f fakeSubscriptions) ResetTraffic(context.Context, *usersub.Subscribe) error {
	return errNotInFacadeTests
}

func (f fakeSubscriptions) SetStatus(context.Context, *usersub.Subscribe, uint8) error {
	return errNotInFacadeTests
}

// noTickets is a support desk without tickets.
type noTickets struct{}

func (noTickets) CountAwaitingReply(context.Context) (int64, error) { return 0, nil }

func (noTickets) List(context.Context, int, int, *uint8) (int64, []*ticket.Ticket, error) {
	return 0, nil, nil
}

func (noTickets) Find(context.Context, int64) (*ticket.Ticket, error) {
	return nil, gorm.ErrRecordNotFound
}

func (noTickets) Detail(context.Context, int64) (*ticket.Details, error) {
	return nil, gorm.ErrRecordNotFound
}

func (noTickets) Reply(context.Context, int64, string, string, bool) (uint8, error) {
	return 0, gorm.ErrRecordNotFound
}

func (noTickets) SetStatus(context.Context, int64, uint8, bool) error {
	return gorm.ErrRecordNotFound
}

// noBilling has no revenue and empty wallets.
type noBilling struct{}

func (noBilling) Revenue(context.Context, time.Time) (int64, error) { return 0, nil }

func (noBilling) Balance(context.Context, int64) (int64, error) { return 0, nil }

// noLogs has no login recorded and drops the rows written to it.
type noLogs struct{}

func (noLogs) RecentLogins(context.Context, int64, int) ([]*log.SystemLog, error) { return nil, nil }

func (noLogs) Insert(context.Context, *log.SystemLog) error { return nil }

const groupID int64 = -1001234

type facadeHarness struct {
	api     *telegramAPI
	service notification.Service
	topics  repository.TelegramTopicRepo
	bot     *tgbot.Bot
	group   int64
}

// newFacade wires the facade to the fake Bot API, the module's own topic
// repository on SQLite and Redis on miniredis. User 7 is bound to chat 1001
// and to buyer@example.com.
func newFacade(t *testing.T) *facadeHarness {
	t.Helper()
	api := &telegramAPI{}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	bot, err := tgbot.New("123456:TEST-TOKEN", tgbot.WithServerURL(server.URL), tgbot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:notification-facade-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&telegramtopic.Topic{}); err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rds.Close() })

	telegramBinding := &user.AuthMethods{Id: 1, UserId: 7, AuthType: "telegram", AuthIdentifier: "1001"}
	h := &facadeHarness{
		api:    api,
		bot:    bot,
		group:  groupID,
		topics: notification.NewRepoBuilder()(repository.ModuleConn{DB: db}).TelegramTopics,
	}
	h.service = notification.New(notification.Deps{
		Bot:         func() *tgbot.Bot { return h.bot },
		GroupChatID: func() int64 { return h.group },
		Topics:      h.topics,
		Redis:       rds,
		Accounts: fakeAccounts{
			byUser: map[string]*user.AuthMethods{
				"telegram:7": telegramBinding,
				"email:7":    {Id: 2, UserId: 7, AuthType: "email", AuthIdentifier: "buyer@example.com"},
			},
			byIdentifier: map[string]*user.AuthMethods{"telegram:1001": telegramBinding},
		},
		Tickets:       noTickets{},
		Subscriptions: fakeSubscriptions{},
		Billing:       noBilling{},
		AuditLogs:     noLogs{},
	})
	return h
}

func TestNotifyTelegramUserSendsMarkdownToTheBoundChat(t *testing.T) {
	h := newFacade(t)

	if err := h.service.NotifyTelegramUser(context.Background(), 7, "*paid*"); err != nil {
		t.Fatalf("NotifyTelegramUser: %v", err)
	}
	sent := h.api.called("sendMessage")
	if len(sent) != 1 || sent[0]["chat_id"] != "1001" || sent[0]["text"] != "*paid*" || sent[0]["parse_mode"] != "MarkdownV2" {
		t.Fatalf("sendMessage calls = %v, want MarkdownV2 to chat 1001", sent)
	}

	// Nothing to deliver to a user without a binding, or without a bot.
	if err := h.service.NotifyTelegramUser(context.Background(), 8, "x"); err == nil {
		t.Fatal("an unbound user reported a delivery")
	}
	h.bot = nil
	if err := h.service.NotifyTelegramUser(context.Background(), 7, "x"); err == nil {
		t.Fatal("an unconfigured bot reported a delivery")
	}
	if len(h.api.called("sendMessage")) != 1 {
		t.Fatal("a message went out without a recipient")
	}
}

func TestNotifyTelegramUnbindRendersTheNotice(t *testing.T) {
	h := newFacade(t)
	if err := h.service.NotifyTelegramUnbind(context.Background(), 7, 1001); err != nil {
		t.Fatalf("NotifyTelegramUnbind: %v", err)
	}
	sent := h.api.called("sendMessage")
	if len(sent) != 1 || sent[0]["chat_id"] != "1001" || sent[0]["parse_mode"] != "MarkdownV2" || !strings.Contains(sent[0]["text"], "7") {
		t.Fatalf("sendMessage calls = %v, want the unbind notice", sent)
	}
}

// The operations feed topic is created on first use and reused after.
func TestNotifyAdminsTelegramPostsIntoTheFeedTopic(t *testing.T) {
	h := newFacade(t)
	for i := 0; i < 2; i++ {
		if err := h.service.NotifyAdminsTelegram(context.Background(), "report"); err != nil {
			t.Fatalf("NotifyAdminsTelegram: %v", err)
		}
	}
	created := h.api.called("createForumTopic")
	if len(created) != 1 || created[0]["chat_id"] != strconv.FormatInt(groupID, 10) || created[0]["name"] != "📣 运营通知" {
		t.Fatalf("createForumTopic calls = %v, want the feed topic once", created)
	}
	sent := h.api.called("sendMessage")
	if len(sent) != 2 || sent[1]["message_thread_id"] != "1" || sent[1]["parse_mode"] != "MarkdownV2" {
		t.Fatalf("sendMessage calls = %v, want both reports in thread 1", sent)
	}

	h.group = 0
	if err := h.service.NotifyAdminsTelegram(context.Background(), "report"); err == nil {
		t.Fatal("a report without a group reported a delivery")
	}
}

// A website ticket gets its topic; website replies and status changes follow
// it there.
func TestTicketTopicFollowsTheTicket(t *testing.T) {
	h := newFacade(t)
	ctx := context.Background()

	if err := h.service.NotifyTicketCreated(ctx, &ticket.Ticket{Id: 5, Title: "cannot connect", Description: "since today", UserId: 7}); err != nil {
		t.Fatalf("NotifyTicketCreated: %v", err)
	}
	created := h.api.called("createForumTopic")
	if len(created) != 1 || created[0]["name"] != "🎫 #5 cannot connect" {
		t.Fatalf("createForumTopic calls = %v", created)
	}
	opening := h.api.called("sendMessage")
	if len(opening) != 1 || !strings.Contains(opening[0]["text"], "buyer@example.com") || !strings.Contains(opening[0]["text"], "since today") {
		t.Fatalf("opening message = %v, want the author and description", opening)
	}

	if err := h.service.NotifyTicketReplied(ctx, 5, ticket.FromUser, "still broken"); err != nil {
		t.Fatalf("NotifyTicketReplied: %v", err)
	}
	if sent := h.api.called("sendMessage"); len(sent) != 2 || sent[1]["text"] != "👤 用户回复：\nstill broken" || sent[1]["message_thread_id"] != "1" {
		t.Fatalf("reply message = %v", sent)
	}

	if err := h.service.NotifyTicketStatusChanged(ctx, 5, ticket.Closed); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := h.service.NotifyTicketStatusChanged(ctx, 5, ticket.Pending); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(h.api.called("closeForumTopic")) != 1 || len(h.api.called("reopenForumTopic")) != 1 {
		t.Fatalf("calls = %+v, want the topic closed then reopened", h.api.calls)
	}
	mapping, err := h.topics.FindByKindRef(ctx, groupID, telegramtopic.KindTicket, 5)
	if err != nil || mapping.Status != telegramtopic.StatusActive {
		t.Fatalf("mapping = %+v (err %v), want active again", mapping, err)
	}

	// Tickets from before the group went live have no topic and are skipped.
	if err := h.service.NotifyTicketReplied(ctx, 99, "admin", "hi"); err != nil {
		t.Fatalf("reply to a ticket without topic: %v", err)
	}
	if err := h.service.NotifyTicketStatusChanged(ctx, 99, ticket.Closed); err != nil {
		t.Fatalf("status of a ticket without topic: %v", err)
	}
}

// The webhook payload is decoded and routed like a polled update.
func TestHandleTelegramWebhookRoutesTheUpdate(t *testing.T) {
	h := newFacade(t)
	payload := `{"update_id":1,"message":{"message_id":3,"date":0,"chat":{"id":1001,"type":"private"},"from":{"id":1001,"is_bot":false,"first_name":"b"},"text":"/help","entities":[{"type":"bot_command","offset":0,"length":5}]}}`

	if err := h.service.HandleTelegramWebhook(context.Background(), []byte(payload)); err != nil {
		t.Fatalf("HandleTelegramWebhook: %v", err)
	}
	sent := h.api.called("sendMessage")
	if len(sent) != 1 || sent[0]["chat_id"] != "1001" || !strings.Contains(sent[0]["text"], "/traffic") {
		t.Fatalf("sendMessage calls = %v, want the private help", sent)
	}
	if err := h.service.HandleTelegramWebhook(context.Background(), []byte("{")); err == nil {
		t.Fatal("a malformed payload was accepted")
	}
}

func TestHandleTelegramUpdateWithoutBotDoesNothing(t *testing.T) {
	h := newFacade(t)
	h.bot = nil
	if err := h.service.HandleTelegramWebhook(context.Background(), []byte(`{"update_id":1,"message":{"message_id":3,"date":0,"chat":{"id":1001,"type":"private"},"text":"hi"}}`)); err != nil {
		t.Fatalf("HandleTelegramWebhook: %v", err)
	}
	if h.api.count() != 0 {
		t.Fatalf("calls = %+v, want none without a bot", h.api.calls)
	}
}

func TestPublishTelegramCommandsPublishesTheUserMenu(t *testing.T) {
	h := newFacade(t)
	if err := h.service.PublishTelegramCommands(context.Background()); err != nil {
		t.Fatalf("PublishTelegramCommands: %v", err)
	}
	published := h.api.called("setMyCommands")
	if len(published) != 1 {
		t.Fatalf("setMyCommands calls = %v", published)
	}
	var commands []struct{ Command string }
	if err := json.Unmarshal([]byte(published[0]["commands"]), &commands); err != nil {
		t.Fatalf("commands %q: %v", published[0]["commands"], err)
	}
	var names []string
	for _, c := range commands {
		names = append(names, c.Command)
	}
	if strings.Join(names, ",") != "start,bind,traffic,help" || published[0]["scope"] != "" {
		t.Fatalf("menu = %v (scope %q), want the default-scope user menu", names, published[0]["scope"])
	}
	h.bot = nil
	if err := h.service.PublishTelegramCommands(context.Background()); err == nil {
		t.Fatal("publishing without a bot reported success")
	}
}

func TestSetupTelegramGroupValidatesTheGroup(t *testing.T) {
	t.Run("no group", func(t *testing.T) {
		h := newFacade(t)
		h.group = 0
		if err := h.service.SetupTelegramGroup(context.Background()); err != nil || h.api.count() != 0 {
			t.Fatalf("error = %v, calls = %d, want a no-op", err, h.api.count())
		}
	})
	t.Run("usable group", func(t *testing.T) {
		h := newFacade(t)
		h.api.chat = `{"id":-1001234,"type":"supergroup","is_forum":true,"accent_color_id":0,"max_reaction_count":0,"accepted_gift_types":{}}`
		h.api.member = `{"status":"administrator","user":{"id":123456,"is_bot":true,"first_name":"bot"},"can_manage_topics":true}`
		if err := h.service.SetupTelegramGroup(context.Background()); err != nil {
			t.Fatalf("SetupTelegramGroup: %v", err)
		}
		if len(h.api.called("createForumTopic")) != 1 {
			t.Fatal("the feed topic was not prepared")
		}
		menus := h.api.called("setMyCommands")
		if len(menus) != 1 || !strings.Contains(menus[0]["scope"], "chat_administrators") || !strings.Contains(menus[0]["commands"], `"toggle"`) {
			t.Fatalf("setMyCommands calls = %v, want the administrators' menu", menus)
		}
	})
	t.Run("not a forum", func(t *testing.T) {
		h := newFacade(t)
		h.api.chat = `{"id":-1001234,"type":"group","accent_color_id":0,"max_reaction_count":0,"accepted_gift_types":{}}`
		if err := h.service.SetupTelegramGroup(context.Background()); err == nil {
			t.Fatal("a group without topics was accepted")
		}
		if len(h.api.called("createForumTopic")) != 0 {
			t.Fatal("a topic was created in an unusable group")
		}
	})
}

func TestRenderTelegramMarkdownEscapesData(t *testing.T) {
	text, err := notification.RenderTelegramMarkdown(notification.AdminOrderNotify, map[string]string{"OrderNo": "20260928-1.2"})
	if err != nil {
		t.Fatalf("RenderTelegramMarkdown: %v", err)
	}
	if !strings.Contains(text, `20260928\-1\.2`) {
		t.Fatalf("text = %q, want the order number escaped", text)
	}
}
