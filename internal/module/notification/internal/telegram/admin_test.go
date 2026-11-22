package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"gorm.io/gorm"
)

const (
	adminChat   = int64(42)
	adminUserID = int64(1)
)

type adminHarness struct {
	admin     *Admin
	messenger *recordingMessenger
	actions   *fakeRedisStore
	accounts  *fakeAccounts
	tickets   *fakeTickets
	subs      *fakeSubscriptions
	billing   fakeBilling
	logs      *fakeAuditLogs
}

// newAdminHarness binds chat 42 to the active administrator user 1.
func newAdminHarness() *adminHarness {
	yes := true
	h := &adminHarness{
		messenger: &recordingMessenger{},
		actions:   &fakeRedisStore{},
		accounts:  newFakeAccounts(),
		tickets:   newFakeTickets(),
		subs:      newFakeSubscriptions(),
		billing:   fakeBilling{balances: map[int64]int64{}},
		logs:      &fakeAuditLogs{},
	}
	h.accounts.users[adminUserID] = &user.User{Id: adminUserID, IsAdmin: &yes, Enable: &yes}
	h.accounts.addBinding(adminUserID, "telegram", "42")
	return h
}

func (h *adminHarness) newAdmin() *Admin {
	return NewAdmin(AdminDependencies{
		Messenger:     h.messenger,
		Actions:       h.actions,
		Accounts:      h.accounts,
		Tickets:       h.tickets,
		Subscriptions: h.subs,
		Billing:       h.billing,
		AuditLogs:     h.logs,
	})
}

func (h *adminHarness) run(text string) string {
	h.admin = h.newAdmin()
	before := len(h.messenger.sent)
	h.admin.Handle(context.Background(), telegramCommand(adminChat, text))
	if len(h.messenger.sent) != before+1 {
		return "<no single reply>"
	}
	return h.messenger.last().message
}

// confirmation extracts the action id of the /confirm_ command a prompt
// offers.
func confirmation(t *testing.T, prompt string) string {
	t.Helper()
	_, rest, ok := strings.Cut(prompt, "/confirm_")
	if !ok {
		t.Fatalf("prompt %q offers no confirmation", prompt)
	}
	return strings.Fields(rest)[0]
}

func TestAdminRejectsUnboundChat(t *testing.T) {
	h := newAdminHarness()
	h.accounts = newFakeAccounts()

	if got := h.run("/help"); !strings.Contains(got, "尚未绑定") {
		t.Fatalf("rejection = %q, want unbound notice", got)
	}
}

// FindUser is unscoped: a soft-deleted or disabled administrator still
// resolves through the Telegram binding and must be refused all the same.
func TestAdminRejectsInactiveAdministrators(t *testing.T) {
	yes, no := true, false
	for name, account := range map[string]*user.User{
		"deleted":      {Id: 1, IsAdmin: &yes, Enable: &yes, DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}},
		"disabled":     {Id: 1, IsAdmin: &yes, Enable: &no},
		"enable unset": {Id: 1, IsAdmin: &yes},
		"not admin":    {Id: 1, IsAdmin: &no, Enable: &yes},
	} {
		t.Run(name, func(t *testing.T) {
			h := newAdminHarness()
			h.accounts.users[adminUserID] = account

			if got := h.run("/help"); !strings.Contains(got, "没有管理权限") {
				t.Fatalf("reply = %q, want the permission refusal", got)
			}
		})
	}
}

func TestAdminAcceptsActiveAdministrator(t *testing.T) {
	if got := newAdminHarness().run("/help"); !strings.Contains(got, "Admin Commands") {
		t.Fatalf("reply = %q, want the admin help", got)
	}
}

func TestAdminBanDisablesTargetAfterConfirmation(t *testing.T) {
	h := newAdminHarness()
	enabled := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &enabled}

	prompt := h.run("/ban 9")
	if !strings.Contains(prompt, "确认禁用用户") {
		t.Fatalf("prompt = %q, want a disable confirmation", prompt)
	}
	if !*h.accounts.users[9].Enable {
		t.Fatal("the prompt already disabled the account")
	}
	actionID := confirmation(t, prompt)
	if got := h.run("/confirm_" + actionID); !strings.Contains(got, "已禁用") {
		t.Fatalf("confirmation reply = %q, want the account disabled", got)
	}
	if *h.accounts.users[9].Enable {
		t.Fatal("the account is still enabled")
	}
	if got := h.run("/confirm_" + actionID); got != "操作已过期或无效。" {
		t.Fatalf("second confirmation = %q, want the confirmation consumed", got)
	}
	if *h.accounts.users[9].Enable {
		t.Fatal("the second confirmation re-enabled the account")
	}
}

// The confirmation applies the change its prompt announced: if the account
// was switched elsewhere meanwhile, it refuses instead of switching it back.
func TestAdminBanConfirmationRefusesAfterStateChange(t *testing.T) {
	for name, tt := range map[string]struct {
		before, meanwhile bool
		prompt, refusal   string
	}{
		"disable": {before: true, meanwhile: false, prompt: "确认禁用用户", refusal: "已处于禁用状态"},
		"enable":  {before: false, meanwhile: true, prompt: "确认启用用户", refusal: "已处于启用状态"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newAdminHarness()
			before := tt.before
			h.accounts.users[9] = &user.User{Id: 9, Enable: &before}
			prompt := h.run("/ban 9")
			if !strings.Contains(prompt, tt.prompt) {
				t.Fatalf("prompt = %q, want %q", prompt, tt.prompt)
			}
			meanwhile := tt.meanwhile
			h.accounts.users[9].Enable = &meanwhile // switched in the panel meanwhile

			actionID := confirmation(t, prompt)
			if got := h.run("/confirm_" + actionID); !strings.Contains(got, tt.refusal) {
				t.Fatalf("reply = %q, want the stale confirmation refused with %q", got, tt.refusal)
			}
			if *h.accounts.users[9].Enable != tt.meanwhile {
				t.Fatal("the stale confirmation switched the account back")
			}
			if _, ok := h.actions.values[actionKey(adminUserID, actionID)]; ok {
				t.Fatal("the refused confirmation is still redeemable")
			}
		})
	}
}

// Two confirmations of one /ban racing each other: the second reads the
// account after the first switched it. It must find the confirmation gone
// instead of switching the account back.
func TestAdminBanConfirmationIsConsumedBeforeItIsApplied(t *testing.T) {
	h := newAdminHarness()
	enabled := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &enabled}
	actionID := confirmation(t, h.run("/ban 9"))

	second := "<not run>"
	h.accounts.afterSetEnabled = func() { second = h.run("/confirm_" + actionID) }
	h.admin.Handle(context.Background(), telegramCommand(adminChat, "/confirm_"+actionID))

	if second != "操作已过期或无效。" {
		t.Fatalf("second confirmation = %q, want it refused", second)
	}
	if first := h.messenger.last().message; !strings.Contains(first, "已禁用") {
		t.Fatalf("first confirmation = %q, want the account disabled", first)
	}
	if *h.accounts.users[9].Enable {
		t.Fatal("the racing confirmation re-enabled the account")
	}
}

func TestAdminCannotBanOwnAccount(t *testing.T) {
	h := newAdminHarness()
	if got := h.run("/ban 1"); !strings.Contains(got, "无法对自己的账号") {
		t.Fatalf("reply = %q, want the self-ban refusal", got)
	}
	if len(h.actions.values) != 0 {
		t.Fatal("a confirmation was issued for the administrator's own account")
	}
}

// A confirmation belongs to the administrator who asked for it, and stays
// redeemable for them, with the window it had, after somebody else tried to
// confirm or cancel it: the other administrator's commands address their own
// actions and find none.
func TestAdminConfirmationIsBoundToItsIssuer(t *testing.T) {
	h := newAdminHarness()
	action, _ := json.Marshal(tgAction{Cmd: "ban", AdminID: 77, Target: "9"})
	foreign := actionKey(77, "foreign")
	h.actions.values = map[string]string{foreign: string(action)}
	h.actions.ttls = map[string]time.Duration{foreign: time.Minute}

	if got := h.run("/confirm_foreign"); got != "操作已过期或无效。" {
		t.Fatalf("reply = %q, want another administrator's confirmation refused", got)
	}
	if got := h.run("/cancel_foreign"); !strings.Contains(got, "已取消") {
		t.Fatalf("reply = %q", got)
	}
	if got := h.actions.values[foreign]; got != string(action) {
		t.Fatalf("stored action = %q, want the other administrator's confirmation kept", got)
	}
	if ttl := h.actions.ttls[foreign]; ttl != time.Minute {
		t.Fatalf("stored action ttl = %v, want its window untouched, not a fresh one", ttl)
	}
	if len(h.actions.deleted) != 1 || h.actions.deleted[0] != actionKey(adminUserID, "foreign") {
		t.Fatalf("deleted = %v, want only the sender's own (missing) action addressed", h.actions.deleted)
	}
}

func TestAdminCancelDropsConfirmation(t *testing.T) {
	h := newAdminHarness()
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive})
	actionID := confirmation(t, h.run("/toggle 5"))

	if got := h.run("/cancel_" + actionID); !strings.Contains(got, "已取消") {
		t.Fatalf("reply = %q, want the cancellation acknowledged", got)
	}
	if got := h.run("/confirm_" + actionID); got != "操作已过期或无效。" {
		t.Fatalf("reply = %q, want the cancelled confirmation gone", got)
	}
	if h.subs.subs[5].Status != usersub.SubscribeStatusActive || h.subs.writes != 0 {
		t.Fatal("a cancelled toggle changed the subscription")
	}
}

// /toggle pauses an active subscription and resumes a paused one, and the
// prompt names the change the confirmation then applies.
func TestAdminToggleSwitchesBetweenActiveAndStopped(t *testing.T) {
	for _, tt := range []struct {
		name         string
		from, to     uint8
		prompt, done string
	}{
		{"pause", usersub.SubscribeStatusActive, usersub.SubscribeStatusStopped, "确认暂停订阅", "已暂停"},
		{"resume", usersub.SubscribeStatusStopped, usersub.SubscribeStatusActive, "确认启用订阅", "已启用"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newAdminHarness()
			h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: tt.from})

			prompt := h.run("/toggle 5")
			if !strings.Contains(prompt, tt.prompt) {
				t.Fatalf("prompt = %q, want %q", prompt, tt.prompt)
			}
			if got := h.run("/confirm_" + confirmation(t, prompt)); !strings.Contains(got, tt.done) {
				t.Fatalf("confirmation reply = %q, want %q", got, tt.done)
			}
			if got := h.subs.subs[5].Status; got != tt.to {
				t.Fatalf("status = %d, want %d", got, tt.to)
			}
		})
	}
}

// Confirming a "pause" on a finished, expired or refunded subscription used
// to reactivate it. Only Active and Stopped are the command's to change.
func TestAdminToggleRefusesOtherStatuses(t *testing.T) {
	for _, status := range []uint8{
		usersub.SubscribeStatusPending, usersub.SubscribeStatusFinished,
		usersub.SubscribeStatusExpired, usersub.SubscribeStatusDeducted,
	} {
		h := newAdminHarness()
		h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: status})

		if got := h.run("/toggle 5"); !strings.Contains(got, "只有活跃或已暂停的订阅可以启停") || strings.Contains(got, "/confirm_") {
			t.Fatalf("status %d: prompt = %q, want a refusal without a confirmation", status, got)
		}

		// A confirmation issued before this change named no status; it must
		// not touch the subscription either.
		action, _ := json.Marshal(tgAction{Cmd: "toggle", AdminID: adminUserID, Target: "5"})
		h.actions.values = map[string]string{actionKey(adminUserID, "legacy"): string(action)}
		if got := h.run("/confirm_legacy"); !strings.Contains(got, "只有活跃或已暂停的订阅可以启停") {
			t.Fatalf("status %d: confirmation reply = %q, want a refusal", status, got)
		}
		if h.subs.subs[5].Status != status || h.subs.writes != 0 {
			t.Fatalf("status %d: subscription changed to %d", status, h.subs.subs[5].Status)
		}
	}
}

// The confirmation applies the change its prompt announced: if the
// subscription moved in between, it refuses instead of flipping it back.
func TestAdminToggleConfirmationRefusesAfterStatusChange(t *testing.T) {
	h := newAdminHarness()
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive})
	prompt := h.run("/toggle 5") // "pause"

	h.subs.subs[5].Status = usersub.SubscribeStatusStopped // paused elsewhere meanwhile
	if got := h.run("/confirm_" + confirmation(t, prompt)); !strings.Contains(got, "状态已变为") {
		t.Fatalf("reply = %q, want the stale confirmation refused", got)
	}
	if h.subs.subs[5].Status != usersub.SubscribeStatusStopped || h.subs.writes != 0 {
		t.Fatal("the stale confirmation resumed the subscription")
	}
}

func TestAdminToggleUnknownSubscription(t *testing.T) {
	h := newAdminHarness()
	if got := h.run("/toggle 5"); got != "订阅不存在。" {
		t.Fatalf("reply = %q", got)
	}
	if got := h.run("/toggle x"); got != "订阅ID格式错误。" {
		t.Fatalf("reply = %q", got)
	}
}

func TestAdminResetZeroesTrafficAfterConfirmation(t *testing.T) {
	h := newAdminHarness()
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive, Download: 3 << 30, Upload: 1 << 30})

	prompt := h.run("/reset 5")
	if !strings.Contains(prompt, "已用：4.0GB") {
		t.Fatalf("prompt = %q, want the used traffic", prompt)
	}
	if h.subs.subs[5].Download == 0 {
		t.Fatal("the prompt already reset the traffic")
	}
	if got := h.run("/confirm_" + confirmation(t, prompt)); !strings.Contains(got, "流量已重置") {
		t.Fatalf("confirmation reply = %q", got)
	}
	if s := h.subs.subs[5]; s.Download != 0 || s.Upload != 0 {
		t.Fatalf("traffic = %d/%d, want reset", s.Download, s.Upload)
	}
}

// A confirmation whose change failed stays redeemable for a retry.
func TestAdminFailedConfirmationStaysRedeemable(t *testing.T) {
	h := newAdminHarness()
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive, Download: 1 << 30})
	actionID := confirmation(t, h.run("/reset 5"))
	missing := h.subs.subs[5]
	delete(h.subs.subs, 5)

	if got := h.run("/confirm_" + actionID); got != "订阅不存在。" {
		t.Fatalf("reply = %q", got)
	}
	h.subs.subs[5] = missing
	if got := h.run("/confirm_" + actionID); !strings.Contains(got, "流量已重置") {
		t.Fatalf("retry = %q, want the confirmation still redeemable", got)
	}
}

func TestAdminReplyTicketRunsTheSupportUseCase(t *testing.T) {
	h := newAdminHarness()
	h.tickets = newFakeTickets(&ticket.Ticket{Id: 321, Status: ticket.Pending})

	got := h.run("/rp 321 请重启客户端")
	if !strings.Contains(got, "已回复工单 #321") || !strings.Contains(got, "待处理 → 🟡 等待用户回复") {
		t.Fatalf("reply = %q, want the previous status shown", got)
	}
	if len(h.tickets.replies) != 1 || h.tickets.replies[0] != (ticketReply{id: 321, from: staffAuthor, content: "请重启客户端"}) {
		t.Fatalf("replies = %+v, want one staff reply mirrored into the topic", h.tickets.replies)
	}
}

func TestAdminReplyTicketValidation(t *testing.T) {
	h := newAdminHarness()
	for text, want := range map[string]string{
		"/rp":         "用法：/rp <工单ID> <回复内容>",
		"/rp 321":     "用法：/rp <工单ID> <回复内容>",
		"/rp x hello": "工单ID格式错误。",
		"/rp 9 hello": "工单不存在。",
	} {
		if got := h.run(text); got != want {
			t.Fatalf("%s: reply = %q, want %q", text, got, want)
		}
	}
	if len(h.tickets.replies) != 0 {
		t.Fatal("an invalid /rp recorded a reply")
	}
}

func TestAdminCloseAndReopenTicket(t *testing.T) {
	h := newAdminHarness()
	h.tickets = newFakeTickets(&ticket.Ticket{Id: 321, Status: ticket.Waiting})

	prompt := h.run("/close 321")
	if !strings.Contains(prompt, "确认关闭工单 #321") {
		t.Fatalf("prompt = %q", prompt)
	}
	if got := h.run("/confirm_" + confirmation(t, prompt)); got != "✅ 工单 #321 已关闭" {
		t.Fatalf("reply = %q", got)
	}
	if got := h.run("/reopen 321"); got != "✅ 工单 #321 已重新打开" {
		t.Fatalf("reply = %q", got)
	}
	want := []ticketStatusChange{{id: 321, status: ticket.Closed}, {id: 321, status: ticket.Pending}}
	if len(h.tickets.statuses) != 2 || h.tickets.statuses[0] != want[0] || h.tickets.statuses[1] != want[1] {
		t.Fatalf("statuses = %+v, want close then reopen, both mirrored", h.tickets.statuses)
	}
	if got := h.run("/reopen 9"); got != "工单不存在。" {
		t.Fatalf("reopen of a missing ticket = %q", got)
	}
	if got := h.run("/close 9"); got != "工单不存在。" {
		t.Fatalf("close of a missing ticket = %q", got)
	}
}

func TestAdminDashboardSummarisesToday(t *testing.T) {
	h := newAdminHarness()
	h.tickets = newFakeTickets(
		&ticket.Ticket{Id: 1, Title: "无法连接", Status: ticket.Pending},
		&ticket.Ticket{Id: 2, Title: "已回复", Status: ticket.Waiting},
	)
	h.tickets.pending = 1
	h.billing.revenue = 12345
	h.accounts.registered = 7

	got := h.run("/dash")
	for _, want := range []string{"待处理工单    1 个", "今日收入       ¥123.45", "今日注册       7 人", "#1 [🔴] 无法连接"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dashboard = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "#2") {
		t.Fatalf("dashboard = %q lists a ticket that awaits the user", got)
	}
}

func TestAdminUserDetailShowsAccountAndSubscriptions(t *testing.T) {
	h := newAdminHarness()
	yes := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &yes, ReferCode: "REF9"}
	h.accounts.addBinding(9, "email", "buyer@example.com")
	h.billing.balances[9] = 2050
	h.subs.byUser[9] = []*usersub.SubscribeDetails{{
		Id: 5, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Pro"},
		Traffic: 10 << 30, Download: 1 << 30, ExpireTime: time.Now().Add(30 * 24 * time.Hour),
	}}

	got := h.run("/user buyer@example.com")
	for _, want := range []string{"ID：9", "邮箱：buyer@example.com", "余额：¥20.50", "推荐码：REF9", "📦 Pro (ID:5)", "流量：1.0/10.0GB", "/toggle_5", "/ban_9 禁用"} {
		if !strings.Contains(got, want) {
			t.Fatalf("user detail = %q, want %q", got, want)
		}
	}
	if got := h.run("/user_sub 9"); !strings.Contains(got, "1. Pro (ID:5)") || !strings.Contains(got, "✅ 活跃") {
		t.Fatalf("user subscriptions = %q", got)
	}
	if got := h.run("/user nobody@example.com"); got != "找不到用户。" {
		t.Fatalf("unknown user = %q", got)
	}
}

// Every "/<command>_<n>" link the bot prints must run its command when it
// is tapped in the admin group, where the update router decides what reaches
// the administrator commands; in the private chat it is redirected like any
// administrator command.
func TestPrintedShortcutLinksRunTheirCommand(t *testing.T) {
	h := newAdminHarness()
	for id := int64(1); id <= 12; id++ {
		h.tickets.tickets[id] = &ticket.Ticket{Id: id, Title: "t", Status: ticket.Pending}
	}
	enabled := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &enabled}
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive})
	bot := NewBot(BotDependencies{
		Messenger: h.messenger, Accounts: h.accounts, Admin: h.newAdmin(),
		GroupChatID: func() int64 { return testGroupID },
	})

	for link, want := range map[string]string{
		"/tickets_2":  "第2/2页",
		"/reset_5":    "确认重置 订阅(ID:5)",
		"/toggle_5":   "确认暂停订阅 (ID:5)",
		"/user_sub_9": "用户无订阅。",
		"/user_log_9": "无登录日志",
		"/ban_9":      "确认禁用用户",
	} {
		before := len(h.messenger.sent)
		bot.HandleUpdate(context.Background(), &models.Update{Message: withCommand(groupMessage(adminChat, 0, link))})
		if len(h.messenger.sent) != before+1 || !strings.Contains(h.messenger.last().message, want) {
			t.Fatalf("%s in the group: sent = %+v, want one reply containing %q", link, h.messenger.sent[before:], want)
		}
	}
	bot.HandleUpdate(context.Background(), privateUpdate(adminChat, "/tickets_2"))
	if got := h.messenger.last().message; !strings.Contains(got, "管理群") {
		t.Fatalf("/tickets_2 in private = %q, want the admin-group redirect", got)
	}
}

func TestAdminUserLogsListsLogins(t *testing.T) {
	h := newAdminHarness()
	h.accounts.users[9] = &user.User{Id: 9}
	login, _ := (&log.Login{Method: "email", LoginIP: "203.0.113.7", Success: true}).Marshal()
	h.logs.logins = []*log.SystemLog{{Content: string(login), CreatedAt: time.Date(2026, 9, 1, 8, 30, 0, 0, time.Local)}}

	if got := h.run("/user_log 9"); !strings.Contains(got, "✅ 09-01 08:30  203.0.113.7  email") {
		t.Fatalf("user logs = %q", got)
	}
}

func TestToggleTarget(t *testing.T) {
	for status, want := range map[uint8]uint8{
		usersub.SubscribeStatusActive:  usersub.SubscribeStatusStopped,
		usersub.SubscribeStatusStopped: usersub.SubscribeStatusActive,
	} {
		if got, ok := toggleTarget(status); !ok || got != want {
			t.Fatalf("toggleTarget(%d) = (%d, %v), want %d", status, got, ok, want)
		}
	}
	for _, status := range []uint8{usersub.SubscribeStatusPending, usersub.SubscribeStatusFinished, usersub.SubscribeStatusExpired, usersub.SubscribeStatusDeducted, 9} {
		if _, ok := toggleTarget(status); ok {
			t.Fatalf("toggleTarget(%d) allowed a status the command must not change", status)
		}
	}
}
