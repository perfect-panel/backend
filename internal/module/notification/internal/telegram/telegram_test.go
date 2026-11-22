package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"gorm.io/gorm"
)

// privateUpdate wraps a command typed in the private chat with chatID.
func privateUpdate(chatID int64, text string) *models.Update {
	return &models.Update{Message: telegramCommand(chatID, text)}
}

// newTrafficBot binds chat 1001 to the enabled user 7.
func newTrafficBot(subs *fakeSubscriptions) (*Bot, *fakeAccounts, *recordingMessenger) {
	yes := true
	accounts := newFakeAccounts()
	accounts.users[7] = &user.User{Id: 7, Enable: &yes}
	accounts.addBinding(7, "telegram", "1001")
	messenger := &recordingMessenger{}
	return NewBot(BotDependencies{Messenger: messenger, Accounts: accounts, Subscriptions: subs}), accounts, messenger
}

// The report lists the subscriptions that can be served: Active ones, and
// the legacy Pending ones that node pulls no longer activate but that are
// served like Active ones. Both the epoch and a NULL expire_time (the zero
// time) mean no time limit; an ended subscription is left out.
func TestTrafficReportsServableSubscriptions(t *testing.T) {
	subs := newFakeSubscriptions()
	expiry := time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC)
	subs.byUser[7] = []*usersub.SubscribeDetails{
		{Id: 1, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Pro"},
			Traffic: 10 << 30, Download: 3 << 30, Upload: 1 << 30, ExpireTime: expiry},
		{Id: 2, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Lifetime"},
			ExpireTime: time.UnixMilli(0)},
		{Id: 3, Status: usersub.SubscribeStatusPending, Subscribe: &subscribe.Subscribe{Name: "Legacy"}},
		{Id: 4, Status: usersub.SubscribeStatusExpired, Subscribe: &subscribe.Subscribe{Name: "Old"}},
	}
	bot, _, messenger := newTrafficBot(subs)

	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/traffic"))

	got := messenger.last()
	if got.chatID != 1001 {
		t.Fatalf("reply went to chat %d", got.chatID)
	}
	for _, want := range []string{"📦 Pro", "已用：4.0GB / 10.0GB", "剩余：6.0GB", "📦 Lifetime", "无限制", "📦 Legacy"} {
		if !strings.Contains(got.message, want) {
			t.Fatalf("report = %q, want %q", got.message, want)
		}
	}
	if strings.Count(got.message, "到期：无限期") != 2 || strings.Contains(got.message, "0000-") {
		t.Fatalf("report = %q, want the epoch and the missing expiry both shown as no time limit", got.message)
	}
	if strings.Contains(got.message, "Old") {
		t.Fatalf("report = %q lists an expired subscription", got.message)
	}
}

// Only subscriptions that can be served now are reported, by the rule the
// node user list applies: a hold, an elapsed term or exhausted traffic keeps
// even a live status out of the report before any job updates it.
func TestTrafficReportSkipsUnservableSubscriptions(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for name, sub := range map[string]*usersub.SubscribeDetails{
		"stopped":   {Status: usersub.SubscribeStatusStopped, ExpireTime: now.Add(time.Hour)},
		"refunded":  {Status: usersub.SubscribeStatusDeducted, ExpireTime: now.Add(time.Hour)},
		"finished":  {Status: usersub.SubscribeStatusFinished, ExpireTime: now.Add(time.Hour)},
		"term over": {Status: usersub.SubscribeStatusActive, ExpireTime: now.Add(-time.Minute)},
		"exhausted": {Status: usersub.SubscribeStatusActive, ExpireTime: now.Add(time.Hour), Traffic: 1 << 30, Download: 1 << 30},
	} {
		if got := trafficReport([]*usersub.SubscribeDetails{sub}, now); got != "您当前没有生效中的订阅。" {
			t.Fatalf("%s: report = %q, want no servable subscription", name, got)
		}
	}
	served := &usersub.SubscribeDetails{Status: usersub.SubscribeStatusPending, ExpireTime: now.Add(time.Hour), Traffic: 1 << 30, Download: 1 << 29}
	if got := trafficReport([]*usersub.SubscribeDetails{served}, now); !strings.Contains(got, "剩余：512MB") {
		t.Fatalf("report = %q, want the servable legacy subscription", got)
	}
}

func TestTrafficWithoutActiveSubscription(t *testing.T) {
	bot, _, messenger := newTrafficBot(newFakeSubscriptions())
	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/traffic"))
	if got := messenger.last().message; got != "您当前没有生效中的订阅。" {
		t.Fatalf("reply = %q", got)
	}
}

// Traffic belongs to the bound account; an unbound chat learns nothing.
func TestTrafficRequiresBinding(t *testing.T) {
	bot, _, messenger := newTrafficBot(newFakeSubscriptions())
	bot.HandleUpdate(context.Background(), privateUpdate(2002, "/traffic"))
	if got := messenger.last(); got.chatID != 2002 || !strings.Contains(got.message, "请先绑定账号") {
		t.Fatalf("reply = %+v, want a bind prompt", got)
	}
}

// The binding alone does not entitle a chat to the report: a disabled or
// soft-deleted account is refused, as the panel refuses it everywhere else.
func TestTrafficRefusesInactiveAccounts(t *testing.T) {
	no := false
	for name, deactivate := range map[string]func(*user.User){
		"disabled": func(u *user.User) { u.Enable = &no },
		"deleted":  func(u *user.User) { u.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true} },
	} {
		t.Run(name, func(t *testing.T) {
			subs := newFakeSubscriptions()
			subs.byUser[7] = []*usersub.SubscribeDetails{{Id: 1, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Pro"}}}
			bot, accounts, messenger := newTrafficBot(subs)
			deactivate(accounts.users[7])

			bot.HandleUpdate(context.Background(), privateUpdate(1001, "/traffic"))

			got := messenger.last()
			if got.chatID != 1001 || !strings.Contains(got.message, "账号已停用") || strings.Contains(got.message, "Pro") {
				t.Fatalf("reply = %+v, want the account refused without its report", got)
			}
		})
	}
}
