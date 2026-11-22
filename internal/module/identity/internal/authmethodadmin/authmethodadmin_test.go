package authmethodadmin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type fixture struct {
	*identitytest.Env
	svc       *Service
	reloaded  []string
	reloadErr error
	senderCfg Snapshot
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	f := &fixture{Env: env}
	f.svc = NewService(Deps{
		Auths:        env.Store.Auth(),
		Config:       func() Snapshot { return f.senderCfg },
		Reinitialize: func(subsystem string) error { f.reloaded = append(f.reloaded, subsystem); return f.reloadErr },
	})
	for _, method := range []string{"email", "mobile", "device", "telegram", "github"} {
		env.EnableMethod(t, method, "{}")
	}
	return f
}

func (f *fixture) stored(t *testing.T, method string) string {
	t.Helper()
	var row auth.Auth
	if err := f.DB.Where("method = ?", method).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.Config
}

// request is an update of method as the admin panel sends it: with the
// method's id and switch.
func (f *fixture) request(t *testing.T, method string, config any) *dto.UpdateAuthMethodConfigRequest {
	t.Helper()
	var row auth.Auth
	if err := f.DB.Where("method = ?", method).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &dto.UpdateAuthMethodConfigRequest{Id: row.Id, Method: method, Config: config, Enabled: row.Enabled}
}

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

// A configuration that does not decode is refused and the stored one kept;
// it used to be replaced by the defaults, wiping the sender settings.
func TestUpdateAuthMethodConfigRefusesConfigsThatDoNotDecode(t *testing.T) {
	f := newFixture(t)
	for method, config := range map[string]any{
		"email":    map[string]any{"enable_verify": "yes"},
		"mobile":   map[string]any{"whitelist": "86"},
		"device":   map[string]any{"enable_security": true},
		"telegram": map[string]any{"enable_notify": "true"},
		"github":   "not an object",
	} {
		_, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, method, config))
		assertCode(t, err, xerr.InvalidParams)
		if got := f.stored(t, method); got != "{}" {
			t.Fatalf("%s config = %s, want it unchanged", method, got)
		}
	}
	if len(f.reloaded) != 0 {
		t.Fatalf("reloaded %v after refused updates", f.reloaded)
	}
}

// A sender reload that fails reaches the administrator; the configuration
// is stored all the same.
func TestUpdateAuthMethodConfigReportsAFailedReload(t *testing.T) {
	f := newFixture(t)
	f.reloadErr = errors.New("reload failed")
	_, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "mobile", map[string]any{"platform": "twilio"}))
	if !errors.Is(err, f.reloadErr) {
		t.Fatalf("UpdateAuthMethodConfig() = %v, want the reload failure", err)
	}
	var stored auth.MobileAuthConfig
	if err := json.Unmarshal([]byte(f.stored(t, "mobile")), &stored); err != nil || stored.Platform != "twilio" {
		t.Fatalf("stored = %+v (%v), want the new configuration", stored, err)
	}
}

func TestUpdateAuthMethodConfigStoresAndReloads(t *testing.T) {
	f := newFixture(t)
	resp, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "mobile",
		map[string]any{"platform": "twilio", "enable_whitelist": true, "whitelist": []any{"86"}}))
	if err != nil {
		t.Fatalf("UpdateAuthMethodConfig() error = %v", err)
	}
	var stored auth.MobileAuthConfig
	if err := json.Unmarshal([]byte(f.stored(t, "mobile")), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Platform != "twilio" || !stored.EnableWhitelist || len(stored.Whitelist) != 1 {
		t.Fatalf("stored = %+v", stored)
	}
	if config, ok := resp.Config.(map[string]any); !ok || config["platform"] != "twilio" {
		t.Fatalf("response config = %#v", resp.Config)
	}
	if len(f.reloaded) != 1 || f.reloaded[0] != "mobile" {
		t.Fatalf("reloaded = %v, want [mobile]", f.reloaded)
	}

	// A provider method keeps what the administrator sent and needs no
	// reload.
	if _, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "github", map[string]any{"client_id": "github-id"})); err != nil {
		t.Fatal(err)
	}
	if got := f.stored(t, "github"); got != `{"client_id":"github-id"}` {
		t.Fatalf("github config = %s", got)
	}
	if len(f.reloaded) != 1 {
		t.Fatalf("reloaded = %v, want no reload for github", f.reloaded)
	}
}

// Without a configuration the method returns to its defaults.
func TestUpdateAuthMethodConfigWithoutConfigResetsTheDefaults(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "email", nil)); err != nil {
		t.Fatal(err)
	}
	var stored auth.EmailAuthConfig
	if err := json.Unmarshal([]byte(f.stored(t, "email")), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.VerifyEmailTemplate == "" || stored.VerifyEmailSubject == "" {
		t.Fatalf("stored = %+v, want the default templates", stored)
	}
}

// An update with a configuration sets the method's switch; a reset to the
// defaults leaves the stored switch as it is.
func TestUpdateAuthMethodConfigSetsTheSwitchWithAConfiguration(t *testing.T) {
	f := newFixture(t)
	storedSwitch := func() bool {
		t.Helper()
		var row auth.Auth
		if err := f.DB.Where("method = ?", "github").First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row.Enabled != nil && *row.Enabled
	}
	off, on := false, true

	req := f.request(t, "github", map[string]any{"client_id": "github-id"})
	req.Enabled = &off
	resp, err := f.svc.UpdateAuthMethodConfig(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Enabled || storedSwitch() {
		t.Fatalf("switch = %t (stored %t), want the method disabled", resp.Enabled, storedSwitch())
	}

	reset := f.request(t, "github", nil)
	reset.Enabled = &on
	resp, err = f.svc.UpdateAuthMethodConfig(context.Background(), reset)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Enabled || storedSwitch() {
		t.Fatalf("switch after the reset = %t (stored %t), want the stored switch kept", resp.Enabled, storedSwitch())
	}
	if got := f.stored(t, "github"); got != new(auth.GithubAuthConfig).Marshal() {
		t.Fatalf("github config = %s, want the defaults", got)
	}
}

// The method names the row: an id of another row is refused rather than
// overwriting that method, and an update without a switch keeps the stored
// one instead of clearing it.
func TestUpdateAuthMethodConfigKeepsTheRowAndItsSwitch(t *testing.T) {
	f := newFixture(t)
	mobile := f.request(t, "mobile", nil)
	wrongRow := f.request(t, "github", map[string]any{"client_id": "github-id"})
	wrongRow.Id = mobile.Id
	_, err := f.svc.UpdateAuthMethodConfig(context.Background(), wrongRow)
	assertCode(t, err, xerr.InvalidParams)
	if got := f.stored(t, "mobile"); got == `{"client_id":"github-id"}` {
		t.Fatalf("the mobile row took the github config: %s", got)
	}

	before := f.request(t, "github", nil).Enabled
	noSwitch := f.request(t, "github", map[string]any{"client_id": "github-id"})
	noSwitch.Id, noSwitch.Enabled = 0, nil
	if _, err := f.svc.UpdateAuthMethodConfig(context.Background(), noSwitch); err != nil {
		t.Fatalf("an update without an id or a switch = %v", err)
	}
	if after := f.request(t, "github", nil).Enabled; after == nil || before == nil || *after != *before {
		t.Fatalf("switch = %v, want the stored %v kept", after, before)
	}
}

// Startup decodes the stored Telegram settings into auth.TelegramAuthConfig,
// so a configuration that does not decode into it is refused on save rather
// than failing the next start, and a saved one reaches the running bot
// through the telegram reload.
func TestUpdateAuthMethodConfigChecksAndReloadsTheTelegramSettings(t *testing.T) {
	f := newFixture(t)
	for name, config := range map[string]any{
		"group chat id as a number": map[string]any{"bot_token": "123456:token", "group_chat_id": -1001234567890},
		"switch as a string":        map[string]any{"bot_token": "123456:token", "enable_notify": "true"},
	} {
		_, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "telegram", config))
		if got := xerr.CodeOf(err); err == nil || got != xerr.InvalidParams {
			t.Fatalf("%s: error = %v (code %d), want code %d", name, err, got, xerr.InvalidParams)
		}
		if got := f.stored(t, "telegram"); got != "{}" {
			t.Fatalf("%s: telegram config = %s, want it unchanged", name, got)
		}
	}
	if len(f.reloaded) != 0 {
		t.Fatalf("reloaded %v after refused updates", f.reloaded)
	}

	resp, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "telegram", map[string]any{
		"bot_token": "123456:token", "enable_notify": true, "webhook_domain": "https://panel.example.com", "group_chat_id": "-1001234567890",
	}))
	if err != nil {
		t.Fatalf("UpdateAuthMethodConfig() error = %v", err)
	}
	var stored auth.TelegramAuthConfig
	if err := json.Unmarshal([]byte(f.stored(t, "telegram")), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.BotToken != "123456:token" || !stored.EnableNotify || stored.WebHookDomain != "https://panel.example.com" || stored.GroupChatID != "-1001234567890" {
		t.Fatalf("stored = %+v", stored)
	}
	if config, ok := resp.Config.(map[string]any); !ok || config["group_chat_id"] != "-1001234567890" {
		t.Fatalf("response config = %#v", resp.Config)
	}
	if len(f.reloaded) != 1 || f.reloaded[0] != "telegram" {
		t.Fatalf("reloaded = %v, want [telegram]", f.reloaded)
	}
}

func TestDeviceConfigNeedsItsSecurityForRealDevices(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "device", map[string]any{"only_real_device": true}))
	assertCode(t, err, xerr.InvalidParams)
	if _, err := f.svc.UpdateAuthMethodConfig(context.Background(), f.request(t, "device",
		map[string]any{"only_real_device": true, "enable_security": true, "security_secret": "key"})); err != nil {
		t.Fatalf("a complete device config was refused: %v", err)
	}
}

func TestAuthMethodListAndConfigDecodeTheStoredConfig(t *testing.T) {
	f := newFixture(t)
	if err := f.DB.Model(&auth.Auth{}).Where("method = ?", "github").Update("config", `{"client_id":"github-id"}`).Error; err != nil {
		t.Fatal(err)
	}
	config, err := f.svc.GetAuthMethodConfig(context.Background(), &dto.GetAuthMethodConfigRequest{Method: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if decoded, ok := config.Config.(map[string]any); !ok || decoded["client_id"] != "github-id" {
		t.Fatalf("config = %#v", config.Config)
	}
	list, err := f.svc.GetAuthMethodList(context.Background())
	if err != nil || len(list.List) != 5 {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err := f.DB.Model(&auth.Auth{}).Where("method = ?", "github").Update("config", `{`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.GetAuthMethodConfig(context.Background(), &dto.GetAuthMethodConfigRequest{Method: "github"})
	assertCode(t, err, xerr.ERROR)
	_, err = f.svc.GetAuthMethodConfig(context.Background(), &dto.GetAuthMethodConfigRequest{Method: "linkedin"})
	assertCode(t, err, xerr.DatabaseQueryError)
}

// A test send that fails tells the administrator why.
func TestTestSendReportsTheSenderFailure(t *testing.T) {
	f := newFixture(t)
	f.senderCfg = Snapshot{EmailPlatform: "no-such-platform", MobilePlatform: "no-such-platform"}
	err := f.svc.TestEmailSend(context.Background(), &dto.TestEmailSendRequest{Email: "admin@example.com"})
	if err == nil {
		t.Fatal("TestEmailSend() with an unknown platform succeeded")
	}
	err = f.svc.TestSmsSend(context.Background(), &dto.TestSmsSendRequest{AreaCode: "86", Telephone: "13800138000"})
	if err == nil {
		t.Fatal("TestSmsSend() with an unknown platform succeeded")
	}
}
