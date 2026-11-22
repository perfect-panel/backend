package bootstrap

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
)

// emptyTelegramToken stores a Telegram auth method without a bot token.
type emptyTelegramToken struct{}

var _ LoginMethods = emptyTelegramToken{}

func (emptyTelegramToken) FindLoginMethod(context.Context, string) (*auth.Auth, error) {
	enabled := false
	return &auth.Auth{
		Method:  "telegram",
		Config:  `{"bot_token":"","enable_notify":false,"webhook_domain":"","group_chat_id":""}`,
		Enabled: &enabled,
	}, nil
}

// panickingNotification is a notification module whose update handler
// panics.
type panickingNotification struct {
	handled int
}

var _ TelegramNotifications = (*panickingNotification)(nil)

func (n *panickingNotification) HandleTelegramUpdate(context.Context, *models.Update) {
	n.handled++
	panic("handler bug")
}

func (n *panickingNotification) PublishTelegramCommands(context.Context) error { return nil }
func (n *panickingNotification) SetupTelegramGroup(context.Context) error      { return nil }

// The polling loop runs handlers without recover: a panic escaping the
// update handler would kill the API process.
func TestTelegramUpdateHandlerContainsPanics(t *testing.T) {
	logs := logtest.NewCollector(t)
	notify := &panickingNotification{}
	handle := telegramUpdateHandler(&Dependencies{Notification: notify})

	handle(context.Background(), nil, &models.Update{ID: 1, Message: &models.Message{Text: "first"}})
	handle(context.Background(), nil, &models.Update{ID: 2, Message: &models.Message{Text: "second"}})
	handle(context.Background(), nil, &models.Update{ID: 3})

	if notify.handled != 2 {
		t.Fatalf("handled = %d, want both message updates dispatched and the empty one skipped", notify.handled)
	}
	if out := logs.String(); !strings.Contains(out, "update handler panicked") || !strings.Contains(out, "handler bug") || !strings.Contains(out, "telegramUpdateHandler") {
		t.Fatalf("log = %s, want the panic logged with its stack", out)
	}
}

// A stored telegram config that does not decode, or no stored telegram
// method at all, is no reason to refuse to start: the load reports it under
// the method's key and goes on without touching the running bot. Startup is
// Migrate followed by the loaders in startupOrder.
func TestStartupToleratesAnUnusableTelegramConfig(t *testing.T) {
	for name, corrupt := range map[string]func(*memStore){
		"undecodable config": func(s *memStore) { s.auth.methods["telegram"].Config = `{"bot_token":` },
		"missing method":     func(s *memStore) { delete(s.auth.methods, "telegram") },
	} {
		t.Run(name, func(t *testing.T) {
			logs := logtest.NewCollector(t)
			store := healthyStore()
			corrupt(store)
			deps, h := newHarness(store, config.Config{Runtime: config.Runtime{Telegram: staleTelegram}})

			if err := loadSubsystems(context.Background(), deps, startupOrder); err != nil {
				t.Fatalf("startup = %v, want the unusable telegram config tolerated", err)
			}
			if h.config.Telegram != staleTelegram {
				t.Fatalf("telegram runtime config = %+v, want the running bot left alone", h.config.Telegram)
			}
			if h.config.Site.SiteName != "PPanel Test" || h.config.Currency.Unit != "USD" {
				t.Fatalf("the other subsystems were not loaded: %+v", h.config)
			}
			if out := logs.String(); !strings.Contains(out, `"method":"telegram"`) {
				t.Fatalf("log = %s, want the rejected telegram method reported under its key", out)
			}
		})
	}
}

// A failed read of the telegram method is a database problem and still
// fails the load, as for every other subsystem.
func TestTelegramReadFailureStillFailsTheLoad(t *testing.T) {
	logtest.Discard(t)
	store := healthyStore()
	store.auth.fail["telegram"] = true
	deps, _ := newHarness(store, config.Config{})

	if err := Telegram(context.Background(), deps); !errors.Is(err, errStoreDown) {
		t.Fatalf("Telegram() = %v, want the store error", err)
	}
}

// Start, end to end on the CI database: the migration runs and a malformed
// telegram config does not stop the server.
func TestStartToleratesAMalformedTelegramConfig(t *testing.T) {
	postgresDSN := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run the startup test")
	}
	logtest.Discard(t)
	store := healthyStore()
	store.auth.methods["telegram"].Config = `{"bot_token":`
	var initial config.Config
	initial.SetDatabaseConfig(*orm.ParseDSN(postgresDSN))
	deps, h := newHarness(store, initial)
	deps.Administrators = &administrators{hasAccounts: true}

	if err := Start(context.Background(), deps); err != nil {
		t.Fatalf("Start() = %v, want the malformed telegram config tolerated", err)
	}
	if h.config.Site.SiteName != "PPanel Test" {
		t.Fatalf("runtime config after Start = %+v, want the stored settings loaded", h.config.Runtime)
	}
}

func TestTelegramEmptyTokenClearsPublishedRuntimeState(t *testing.T) {
	runtimeConfig := config.Config{Runtime: config.Runtime{Telegram: config.Telegram{
		Enable:        true,
		BotID:         123,
		BotName:       "old-bot",
		BotToken:      "old-token",
		EnableNotify:  true,
		WebHookDomain: "https://old.example.com",
		GroupChatID:   -100123,
	}}}
	botSetterCalled := false
	publishedBot := new(tgbot.Bot)
	deps := &Dependencies{
		Config: func() config.Config { return runtimeConfig },
		UpdateRuntime: func(update func(*config.Runtime)) {
			update(&runtimeConfig.Runtime)
		},
		LoginMethods: emptyTelegramToken{},
		SetTelegramBot: func(bot *tgbot.Bot) {
			botSetterCalled = true
			publishedBot = bot
		},
	}

	if err := Telegram(context.Background(), deps); err != nil {
		t.Fatalf("Telegram() = %v", err)
	}

	if !botSetterCalled {
		t.Fatal("Telegram() did not revoke the published bot client")
	}
	if publishedBot != nil {
		t.Fatal("Telegram() retained the previous bot client after clearing the token")
	}
	if !reflect.DeepEqual(runtimeConfig.Telegram, config.Telegram{}) {
		t.Fatalf("runtime Telegram config = %#v, want zero config", runtimeConfig.Telegram)
	}
}
