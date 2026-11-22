package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// Two chats opening the same forwarded deep link: the token is consumed in
// one step before anything is checked, so the second chat finds nothing to
// redeem and only the first is bound. (In webhook mode the two updates are
// handled concurrently; the fake store consumes like GETDEL does.)
func TestBindTokenIsConsumedBeforeBindingSoTwoChatsCannotBothRedeemIt(t *testing.T) {
	logtest.Discard(t)
	bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey("shared"): "7"})
	// The second chat's redemption runs while the first one's binding is
	// being recorded: after the token was consumed, before the binding is
	// visible.
	second := ""
	accounts.beforeBind = func() {
		bot.HandleUpdate(context.Background(), privateUpdate(2002, "/start shared"))
		second = messenger.last().message
	}

	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start shared"))

	if second != "Bind token is invalid or expired. Please request a new one." {
		t.Fatalf("second chat = %q, want the consumed token refused", second)
	}
	if len(accounts.bound) != 1 || accounts.bound[0].AuthIdentifier != "1001" {
		t.Fatalf("bindings = %+v, want only the first chat bound", accounts.bound)
	}
	if _, present := store.values[bindKey("shared")]; present {
		t.Fatal("the redeemed token was put back")
	}
}

// Two links of one account redeemed from two chats at once: the binding
// runs under a lock on the account, so the second redemption finds the lock
// held, is refused with its token put back, and only one binding exists.
func TestConcurrentRedemptionsOfOneAccountBindOnce(t *testing.T) {
	logtest.Discard(t)
	bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey("first"): "7", bindKey("second"): "7"})
	second := ""
	accounts.beforeBind = func() {
		bot.HandleUpdate(context.Background(), privateUpdate(2002, "/start second"))
		second = messenger.last().message
	}

	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start first"))

	if second != bindFailed {
		t.Fatalf("second chat = %q, want it refused while the account's binding is in progress", second)
	}
	if len(accounts.bound) != 1 || accounts.bound[0].AuthIdentifier != "1001" {
		t.Fatalf("bindings = %+v, want exactly one", accounts.bound)
	}
	if value, present := store.values[bindKey("second")]; !present || value != "7" || store.ttls[bindKey("second")] != fakeTokenTTL {
		t.Fatalf("second token = %q (present %v, ttl %v), want it put back with its remaining life for a retry", value, present, store.ttls[bindKey("second")])
	}
	if _, locked := store.values[bindLockKey(7)]; locked {
		t.Fatal("the binding lock was not released")
	}
	// The retry, once the first redemption finished, sees the binding.
	bot.HandleUpdate(context.Background(), privateUpdate(2002, "/start second"))
	if got := messenger.last().message; got != "Your account is already bound to a different Telegram account. Please unbind it first." {
		t.Fatalf("retry = %q", got)
	}
	if len(accounts.bound) != 1 {
		t.Fatal("the retry bound the account a second time")
	}
}

// A failure of the stores after the token was consumed puts the token back
// with its remaining life; the user retries without a new link.
func TestBindRestoresTheTokenWhenTheStoreFails(t *testing.T) {
	logtest.Discard(t)
	bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey("tok"): "7"})
	store.ttls = map[string]time.Duration{bindKey("tok"): 90 * time.Second}
	accounts.err = errors.New("identity store down")

	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start tok"))

	if got := messenger.last().message; got != bindFailed {
		t.Fatalf("message = %q", got)
	}
	if value := store.values[bindKey("tok")]; value != "7" || store.ttls[bindKey("tok")] != 90*time.Second {
		t.Fatalf("token = %q with ttl %v, want it restored with the 90s it had left", value, store.ttls[bindKey("tok")])
	}
	// Once the store is back the same link binds.
	accounts.err = nil
	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start tok"))
	if len(accounts.bound) != 1 {
		t.Fatalf("bindings = %+v, want the retried link redeemed", accounts.bound)
	}
}

// A token store that cannot be reached refuses the bind without touching
// the account.
func TestBindFailsClosedWithoutTheTokenStore(t *testing.T) {
	logtest.Discard(t)
	bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey("tok"): "7"})
	store.err = errors.New("redis down")
	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start tok"))
	if got := messenger.last().message; got != bindFailed || len(accounts.bound) != 0 {
		t.Fatalf("message = %q, bindings = %+v", got, accounts.bound)
	}
}

// Every mutation made through the bot leaves an audit row filed under the
// bound administrator account, with the Telegram sender next to it; the
// reads leave none.
func TestAdminMutationsAreAudited(t *testing.T) {
	h := newAdminHarness()
	enabled := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &enabled}
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, UserId: 9, Status: usersub.SubscribeStatusActive, Download: 1 << 30})
	h.tickets = newFakeTickets(&ticket.Ticket{Id: 321, Status: ticket.Pending})

	h.run("/confirm_" + confirmation(t, h.run("/ban 9")))
	h.run("/confirm_" + confirmation(t, h.run("/reset 5")))
	h.run("/confirm_" + confirmation(t, h.run("/toggle 5")))
	h.run("/rp 321 on it")
	h.run("/confirm_" + confirmation(t, h.run("/close 321")))
	h.run("/reopen 321")
	h.run("/user 9")
	h.run("/dash")

	actions := h.logs.actions(t)
	want := []struct {
		action, object string
		id             int64
		detail         string
	}{
		{"user.ban", "user", 9, "enabled=false"},
		{"subscription.reset_traffic", "user_subscribe", 5, "user_id=9"},
		{"subscription.status", "user_subscribe", 5, "user_id=9 status=" + strconv.Itoa(int(usersub.SubscribeStatusStopped))},
		{"ticket.reply", "ticket", 321, ""},
		{"ticket.status", "ticket", 321, "status=4"},
		{"ticket.status", "ticket", 321, "status=1"},
	}
	if len(actions) != len(want) {
		t.Fatalf("audited %d actions, want %d: %+v", len(actions), len(want), actions)
	}
	for i, tc := range want {
		got := actions[i]
		if got.Action != tc.action || got.Object != tc.object || got.ObjectID != tc.id || got.Detail != tc.detail ||
			got.ActorID != adminUserID || got.TelegramSenderID != adminChat || got.Source != log.AdminActionSourceTelegram || got.Timestamp == 0 {
			t.Fatalf("action %d = %+v, want %+v by administrator %d from Telegram sender %d", i, got, tc, adminUserID, adminChat)
		}
		if h.logs.rows[i].ObjectID != adminUserID {
			t.Fatalf("row %d filed under %d, want the administrator", i, h.logs.rows[i].ObjectID)
		}
	}
}

// A subscription without a time limit shows as such in the user detail and
// the subscription list, instead of a negative number of days about to
// expire.
func TestAdminUserViewsShowNoExpiryAsUnlimited(t *testing.T) {
	h := newAdminHarness()
	yes := true
	h.accounts.users[9] = &user.User{Id: 9, Enable: &yes}
	h.subs.byUser[9] = []*usersub.SubscribeDetails{
		{Id: 5, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Lifetime"}, Traffic: 10 << 30, ExpireTime: time.UnixMilli(0)},
		{Id: 6, Status: usersub.SubscribeStatusActive, Subscribe: &subscribe.Subscribe{Name: "Monthly"}, Traffic: 10 << 30, ExpireTime: time.Now().Add(2 * 24 * time.Hour)},
	}

	detail := h.run("/user 9")
	if !strings.Contains(detail, "到期：无限期") || strings.Contains(detail, "剩-") {
		t.Fatalf("user detail = %q, want the lifetime subscription shown without a time limit", detail)
	}
	if !strings.Contains(detail, "⚠️即将过期") || strings.Count(detail, "⚠️") != 1 {
		t.Fatalf("user detail = %q, want only the monthly subscription flagged as expiring", detail)
	}
	if list := h.run("/user_sub 9"); !strings.Contains(list, "到期：无限期") || strings.Contains(list, "1970") {
		t.Fatalf("subscription list = %q, want the lifetime subscription shown without a time limit", list)
	}
}

// /cancel_ addresses the sender's own confirmation only: cancelling another
// administrator's action by its id changes nothing.
func TestAdminCancelIsBoundToItsIssuer(t *testing.T) {
	h := newAdminHarness()
	h.subs = newFakeSubscriptions(&usersub.Subscribe{Id: 5, Status: usersub.SubscribeStatusActive})
	actionID := confirmation(t, h.run("/toggle 5"))

	// Another administrator, bound to chat 43 as user 2, cancels it.
	yes := true
	h.accounts.users[2] = &user.User{Id: 2, IsAdmin: &yes, Enable: &yes}
	h.accounts.addBinding(2, "telegram", "43")
	h.admin = h.newAdmin()
	h.admin.Handle(context.Background(), telegramCommand(43, "/cancel_"+actionID))

	if got := h.run("/confirm_" + actionID); !strings.Contains(got, "已暂停") {
		t.Fatalf("issuer's confirmation = %q, want it still redeemable after another administrator's cancel", got)
	}
}
