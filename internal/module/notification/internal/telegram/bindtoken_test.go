package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
)

func newBindHarness(tokens map[string]string) (*Bot, *fakeRedisStore, *fakeAccounts, *recordingMessenger) {
	store := &fakeRedisStore{values: tokens}
	accounts := newFakeAccounts()
	messenger := &recordingMessenger{}
	bot := NewBot(BotDependencies{
		Messenger: messenger,
		Sessions:  store,
		Accounts:  accounts,
	})
	return bot, store, accounts, messenger
}

func bindKey(token string) string {
	return fmt.Sprintf("%v:%v", config.TelegramBindKey, token)
}

// A session id must no longer work as a bind token: the deep link travels
// through Telegram chats, and accepting session ids let whoever saw one bind
// their own Telegram account — and therefore log in — as that user.
func TestBindRejectsSessionIdAsToken(t *testing.T) {
	for _, command := range []string{"/bind", "/start"} {
		token := "leaked-session-id"
		bot, _, accounts, messenger := newBindHarness(map[string]string{
			fmt.Sprintf("%v:%v", config.SessionIdKey, token): "7",
		})

		bot.HandleUpdate(context.Background(), privateUpdate(1001, command+" "+token))

		if len(accounts.bound) != 0 {
			t.Fatalf("%s: session id was accepted as a bind token: %+v", command, accounts.bound[0])
		}
		if got := messenger.last().message; got != "Bind token is invalid or expired. Please request a new one." {
			t.Fatalf("%s: message = %q", command, got)
		}
	}
}

// A dedicated bind token binds the account once and is invalidated, so a link
// that leaks after use cannot rebind the account. /start (the deep link) and
// /bind share the one implementation.
func TestBindConsumesDedicatedTokenExactlyOnce(t *testing.T) {
	for _, command := range []string{"/bind", "/start"} {
		t.Run(command, func(t *testing.T) {
			token := "bind-token"
			bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey(token): "7"})

			bot.HandleUpdate(context.Background(), privateUpdate(1001, command+" "+token))

			if len(accounts.bound) != 1 {
				t.Fatalf("bindings = %d, want 1", len(accounts.bound))
			}
			if got := accounts.bound[0]; got.UserId != 7 || got.AuthIdentifier != "1001" {
				t.Fatalf("binding = %+v, want user 7 bound to chat 1001", got)
			}
			if _, present := store.values[bindKey(token)]; present {
				t.Fatal("the redeemed token is still stored")
			}
			// The binding lock is released; nothing else is left behind.
			if len(store.values) != 0 || len(store.deleted) != 1 || store.deleted[0] != bindLockKey(7) {
				t.Fatalf("store = %v, deleted = %v; want the lock released and the token consumed", store.values, store.deleted)
			}
			if !messenger.last().markdown {
				t.Fatal("bind confirmation must be sent as MarkdownV2, not plain text")
			}

			// Replaying the same link finds nothing to redeem.
			bot.HandleUpdate(context.Background(), privateUpdate(2002, command+" "+token))
			if len(accounts.bound) != 1 {
				t.Fatal("a consumed bind token was accepted again")
			}
			if got := messenger.last().message; got != "Bind token is invalid or expired. Please request a new one." {
				t.Fatalf("message = %q", got)
			}
		})
	}
}

func TestBindWithoutTokenPromptsPerEntryPoint(t *testing.T) {
	for command, want := range map[string]string{
		"/bind":  "Please provide a bind token. Usage: /bind <token>",
		"/start": "Please bind account!",
	} {
		bot, _, _, messenger := newBindHarness(nil)
		bot.HandleUpdate(context.Background(), privateUpdate(42, command))
		if got := messenger.last(); got.chatID != 42 || got.message != want {
			t.Fatalf("%s: message = (%d, %q), want %q", command, got.chatID, got.message, want)
		}
	}
}

// One Telegram account binds one panel account and the other way round; an
// existing binding is never overwritten. A token refused for a reason the
// user can fix is put back with the life it had left, so the user retries
// without a new link and the link's life is not extended; a token that finds
// its account already bound here has done its work and stays consumed.
func TestBindKeepsExistingBindings(t *testing.T) {
	for name, tt := range map[string]struct {
		seed     func(*fakeAccounts)
		want     string
		restored bool
	}{
		"chat bound elsewhere": {
			seed:     func(a *fakeAccounts) { a.addBinding(8, "telegram", "1001") },
			want:     "This Telegram account is already bound to another user.",
			restored: true,
		},
		"account bound to another chat": {
			seed:     func(a *fakeAccounts) { a.addBinding(7, "telegram", "5005") },
			want:     "Your account is already bound to a different Telegram account. Please unbind it first.",
			restored: true,
		},
		"already bound here": {
			seed: func(a *fakeAccounts) { a.addBinding(7, "telegram", "1001") },
			want: "This account is already bound to your Telegram.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			bot, store, accounts, messenger := newBindHarness(map[string]string{bindKey("tok"): "7"})
			tt.seed(accounts)

			bot.HandleUpdate(context.Background(), privateUpdate(1001, "/bind tok"))

			if len(accounts.bound) != 0 {
				t.Fatalf("binding written: %+v", accounts.bound)
			}
			if got := messenger.last().message; got != tt.want {
				t.Fatalf("message = %q, want %q", got, tt.want)
			}
			value, present := store.values[bindKey("tok")]
			if present != tt.restored || (present && (value != "7" || store.ttls[bindKey("tok")] != fakeTokenTTL)) {
				t.Fatalf("token stored = %v (%q, ttl %v), want restored %v with its remaining life", present, value, store.ttls[bindKey("tok")], tt.restored)
			}
			if _, locked := store.values[bindLockKey(7)]; locked {
				t.Fatal("the binding lock was not released")
			}
		})
	}
}

func TestBindRejectsMalformedTokenValue(t *testing.T) {
	bot, _, accounts, messenger := newBindHarness(map[string]string{bindKey("tok"): "not-a-user"})
	bot.HandleUpdate(context.Background(), privateUpdate(1001, "/start tok"))
	if len(accounts.bound) != 0 || !strings.Contains(messenger.last().message, "Invalid session data") {
		t.Fatalf("bound = %+v, message = %q", accounts.bound, messenger.last().message)
	}
}
