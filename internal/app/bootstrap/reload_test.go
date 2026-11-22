package bootstrap

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

var errStoreDown = errors.New("store unavailable")

// memSystem serves stored settings from memory. A category listed in fail
// answers with errStoreDown.
type memSystem struct {
	settings      map[string][]*system.System
	fail          map[string]bool
	multiplier    *system.System
	multiplierErr error
	updates       map[string]string
}

var (
	_ Settings     = (*memSystem)(nil)
	_ NodeSettings = (*memSystem)(nil)
)

func (r *memSystem) read(category string) ([]*system.System, error) {
	if r.fail[category] {
		return nil, errStoreDown
	}
	return r.settings[category], nil
}

func (r *memSystem) GetSiteConfig(context.Context) ([]*system.System, error) {
	return r.read(categorySite)
}
func (r *memSystem) GetInviteConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryInvite)
}
func (r *memSystem) GetRegisterConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryRegister)
}
func (r *memSystem) GetSubscribeConfig(context.Context) ([]*system.System, error) {
	return r.read(categorySubscribe)
}
func (r *memSystem) GetVerifyConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryVerify)
}
func (r *memSystem) GetVerifyCodeConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryVerifyCode)
}
func (r *memSystem) GetNodeConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryNode)
}
func (r *memSystem) GetCurrencyConfig(context.Context) ([]*system.System, error) {
	return r.read(categoryCurrency)
}

func (r *memSystem) FindNodeMultiplierConfig(context.Context) (*system.System, error) {
	if r.multiplierErr != nil {
		return nil, r.multiplierErr
	}
	return r.multiplier, nil
}

func (r *memSystem) Insert(_ context.Context, data *system.System) error {
	r.multiplier = data
	r.multiplier.Id = 99
	return nil
}

func (r *memSystem) UpdateValueByCategoryKey(_ context.Context, category, key, value string, _ ...string) error {
	if r.updates == nil {
		r.updates = make(map[string]string)
	}
	r.updates[category+"."+key] = value
	return nil
}

// memAuth serves the stored auth-method settings from memory, as the
// identity facade does; a method listed in fail answers with errStoreDown.
type memAuth struct {
	methods map[string]*auth.Auth
	fail    map[string]bool
}

var _ LoginMethods = (*memAuth)(nil)

func (r *memAuth) FindLoginMethod(_ context.Context, method string) (*auth.Auth, error) {
	if r.fail[method] {
		return nil, errStoreDown
	}
	return r.methods[method], nil
}

// memStore is the stored state the loaders read: the platform settings in
// system, served to the transaction as well, and in auth the identity-owned
// auth-method settings behind LoginMethods. txErr fails the transaction.
type memStore struct {
	system *memSystem
	auth   *memAuth
	txErr  error
}

var _ SettingsTransactor = (*memStore)(nil)

func (s *memStore) InSettingsTx(_ context.Context, fn func(NodeSettings) error) error {
	if s.txErr != nil {
		return s.txErr
	}
	return fn(s.system)
}

// settingsDeps are the dependencies of the loaders that read only the stored
// settings.
func (s *memStore) settingsDeps() *Dependencies {
	return &Dependencies{Settings: s.system, SettingsTx: s}
}

func setting(category, key, value, typ string) *system.System {
	return &system.System{Category: category, Key: key, Value: value, Type: typ}
}

func enabled(value bool) *bool { return &value }

// healthyStore holds one distinctive value per subsystem, so a test can tell
// which subsystems a load touched.
func healthyStore() *memStore {
	return &memStore{
		system: &memSystem{
			settings: map[string][]*system.System{
				categorySite:      {setting(categorySite, "SiteName", "PPanel Test", "string")},
				categoryInvite:    {setting(categoryInvite, "ReferralPercentage", "20", "int")},
				categoryRegister:  {setting(categoryRegister, "StopRegister", "true", "bool")},
				categorySubscribe: {setting(categorySubscribe, "SubscribePath", "/sub", "string")},
				categoryVerify:    {setting(categoryVerify, "EnableLoginVerify", "true", "bool")},
				categoryVerifyCode: {
					setting(categoryVerifyCode, "VerifyCodeLimit", "5", "int"),
				},
				categoryNode: {
					setting(categoryNode, "NodeSecret", "configured-secret", "string"),
					setting(categoryNode, "NodePullInterval", "30", "int"),
					setting(categoryNode, "DNS", `[{"proto":"udp","address":"1.1.1.1","domains":["example.com"]}]`, "string"),
				},
				categoryCurrency: {setting(categoryCurrency, "CurrencyUnit", "USD", "string")},
			},
			fail:       map[string]bool{},
			multiplier: setting(categoryNode, nodeMultiplierKey, "[]", "string"),
		},
		auth: &memAuth{
			methods: map[string]*auth.Auth{
				"email":    {Method: "email", Config: `{"platform":"smtp","platform_config":{"host":"smtp.example.com"}}`, Enabled: enabled(true)},
				"mobile":   {Method: "mobile", Config: `{"platform":"AlibabaCloud","platform_config":{}}`, Enabled: enabled(true)},
				"device":   {Method: "device", Config: `{"enable_security":true,"security_secret":"device-secret"}`, Enabled: enabled(true)},
				"telegram": {Method: "telegram", Config: `{"bot_token":""}`, Enabled: enabled(false)},
			},
			fail: map[string]bool{},
		},
	}
}

type runtimeHarness struct {
	config          config.Config
	managerSettings int
}

func newHarness(store *memStore, initial config.Config) (*Dependencies, *runtimeHarness) {
	h := &runtimeHarness{config: initial}
	deps := &Dependencies{
		Config:        func() config.Config { return h.config },
		UpdateRuntime: func(update func(*config.Runtime)) { update(&h.config.Runtime) },
		Settings:      store.system,
		SettingsTx:    store,
		LoginMethods:  store.auth,
		ExchangeRate:  billing.NewCurrencyRateCache(1),
		SetNodeMultiplierManager: func(*network.MultiplierManager) {
			h.managerSettings++
		},
	}
	return deps, h
}

// changedSections names the runtime sections that differ between before and
// after, and "Boot" when a boot setting does.
func changedSections(before, after config.Config) []string {
	var changed []string
	if !reflect.DeepEqual(before.Boot, after.Boot) {
		changed = append(changed, "Boot")
	}
	b, a := reflect.ValueOf(before.Runtime), reflect.ValueOf(after.Runtime)
	for i := 0; i < b.NumField(); i++ {
		if !reflect.DeepEqual(b.Field(i).Interface(), a.Field(i).Interface()) {
			changed = append(changed, b.Type().Field(i).Name)
		}
	}
	sort.Strings(changed)
	return changed
}

// staleTelegram stands in for a running bot, so clearing it shows as a change.
var staleTelegram = config.Telegram{Enable: true, BotToken: "old-token", BotName: "old-bot"}

// The admin settings handlers name subsystems with these strings; each must
// reach its own loader and nothing else.
func TestReloadDispatchesTheNamesAdminHandlersSend(t *testing.T) {
	logtest.Discard(t)
	cases := map[string][]string{
		"site":      {"Site"},
		"node":      {"Node"},
		"email":     {"Email"},
		"device":    {"Device"},
		"invite":    {"Invite"},
		"verify":    {"Verify", "VerifyCode"},
		"subscribe": {"Subscribe"},
		"register":  {"Register"},
		"mobile":    {"Mobile"},
		"currency":  {"Currency"},
		"telegram":  {"Telegram"},
	}
	if len(cases) != len(loaders) {
		t.Fatalf("%d reloadable subsystems, %d covered here", len(loaders), len(cases))
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			before := config.Config{Runtime: config.Runtime{Telegram: staleTelegram}}
			deps, h := newHarness(healthyStore(), before)

			if err := Reload(context.Background(), deps, Subsystem(name)); err != nil {
				t.Fatalf("Reload(%q) = %v", name, err)
			}
			if got := changedSections(before, h.config); !reflect.DeepEqual(got, want) {
				t.Fatalf("Reload(%q) changed %v, want %v", name, got, want)
			}
		})
	}
}

// A misspelt subsystem used to fall through the switch and silently do
// nothing.
func TestReloadRejectsUnknownSubsystems(t *testing.T) {
	logs := logtest.NewCollector(t)
	before := config.Config{Runtime: config.Runtime{Telegram: staleTelegram}}
	deps, h := newHarness(healthyStore(), before)

	err := Reload(context.Background(), deps, Subsystem("sites"))

	if !errors.Is(err, ErrUnknownSubsystem) || !strings.Contains(err.Error(), `"sites"`) {
		t.Fatalf("Reload(sites) = %v, want ErrUnknownSubsystem naming it", err)
	}
	if changed := changedSections(before, h.config); len(changed) != 0 {
		t.Fatalf("an unknown subsystem changed %v", changed)
	}
	if !strings.Contains(logs.String(), "unknown subsystem") {
		t.Fatalf("log = %s, want the unknown subsystem reported", logs.String())
	}
}

// Six loaders used to panic on a database error during an admin reload while
// the others logged. Every one now returns the error, logs it and keeps the
// configuration that was already running.
func TestReloadKeepsThePreviousConfigurationWhenTheStoreFails(t *testing.T) {
	failures := map[Subsystem]func(*memStore){
		SubsystemSite:      func(s *memStore) { s.system.fail[categorySite] = true },
		SubsystemNode:      func(s *memStore) { s.system.fail[categoryNode] = true },
		SubsystemEmail:     func(s *memStore) { s.auth.fail["email"] = true },
		SubsystemDevice:    func(s *memStore) { s.auth.fail["device"] = true },
		SubsystemInvite:    func(s *memStore) { s.system.fail[categoryInvite] = true },
		SubsystemVerify:    func(s *memStore) { s.system.fail[categoryVerify] = true },
		SubsystemSubscribe: func(s *memStore) { s.system.fail[categorySubscribe] = true },
		SubsystemRegister:  func(s *memStore) { s.system.fail[categoryRegister] = true },
		SubsystemMobile:    func(s *memStore) { s.auth.fail["mobile"] = true },
		SubsystemCurrency:  func(s *memStore) { s.system.fail[categoryCurrency] = true },
		SubsystemTelegram:  func(s *memStore) { s.auth.fail["telegram"] = true },
	}
	if len(failures) != len(loaders) {
		t.Fatalf("%d reloadable subsystems, %d covered here", len(loaders), len(failures))
	}
	for subsystem, fail := range failures {
		t.Run(string(subsystem), func(t *testing.T) {
			logs := logtest.NewCollector(t)
			store := healthyStore()
			deps, h := newHarness(store, config.Config{})
			if err := loadSubsystems(context.Background(), deps, startupOrder); err != nil {
				t.Fatalf("healthy load failed: %v", err)
			}
			h.config.Telegram = staleTelegram
			previous := h.config
			fail(store)

			err := Reload(context.Background(), deps, subsystem)

			if !errors.Is(err, errStoreDown) {
				t.Fatalf("Reload(%s) = %v, want the store error", subsystem, err)
			}
			if changed := changedSections(previous, h.config); len(changed) != 0 {
				t.Fatalf("a failed reload changed %v", changed)
			}
			if !strings.Contains(logs.String(), "keeping the previous configuration") {
				t.Fatalf("log = %s, want the failure reported", logs.String())
			}
		})
	}
}

// Verify publishes the captcha and verification-code settings together; a
// failure reading the second must not leave the first half-applied.
func TestVerifyReloadIsAllOrNothing(t *testing.T) {
	logtest.Discard(t)
	store := healthyStore()
	store.system.fail[categoryVerifyCode] = true
	deps, h := newHarness(store, config.Config{})

	if err := Reload(context.Background(), deps, SubsystemVerify); !errors.Is(err, errStoreDown) {
		t.Fatalf("Reload(verify) = %v, want the store error", err)
	}
	if h.config.Verify.LoginVerify {
		t.Fatal("captcha settings published although the verification-code read failed")
	}
}

// A stored DNS list that is not JSON used to panic the process; the load now
// fails and the previous node configuration and multiplier stay.
func TestNodeReloadRejectsMalformedSettings(t *testing.T) {
	cases := map[string]func(*memStore){
		"dns": func(s *memStore) {
			s.system.settings[categoryNode] = append(s.system.settings[categoryNode], setting(categoryNode, "DNS", "not json", "string"))
		},
		"outbound": func(s *memStore) {
			s.system.settings[categoryNode] = append(s.system.settings[categoryNode], setting(categoryNode, "Outbound", "{", "string"))
		},
		"multiplier read": func(s *memStore) { s.system.multiplierErr = errStoreDown },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			logtest.Discard(t)
			store := healthyStore()
			deps, h := newHarness(store, config.Config{})
			if err := Reload(context.Background(), deps, SubsystemNode); err != nil {
				t.Fatalf("healthy node reload failed: %v", err)
			}
			previous, managers := h.config, h.managerSettings
			corrupt(store)

			if err := Reload(context.Background(), deps, SubsystemNode); err == nil {
				t.Fatal("Reload(node) accepted malformed settings")
			}
			if changed := changedSections(previous, h.config); len(changed) != 0 || h.managerSettings != managers {
				t.Fatalf("a failed node reload changed %v (multiplier replaced: %v)", changed, h.managerSettings != managers)
			}
		})
	}
}

// deviceConfig.Unmarshal's error used to be ignored, silently switching
// request signing off.
func TestDeviceReloadRejectsAnUndecodableConfig(t *testing.T) {
	logtest.Discard(t)
	store := healthyStore()
	deps, h := newHarness(store, config.Config{})
	if err := Reload(context.Background(), deps, SubsystemDevice); err != nil || !h.config.Device.EnableSecurity {
		t.Fatalf("healthy device reload: err=%v config=%+v", err, h.config.Device)
	}
	store.auth.methods["device"].Config = `{"enable_security":`

	if err := Reload(context.Background(), deps, SubsystemDevice); err == nil {
		t.Fatal("Reload(device) accepted an undecodable config")
	}
	if !h.config.Device.EnableSecurity || h.config.Device.SecuritySecret != "device-secret" {
		t.Fatalf("device security settings lost: %+v", h.config.Device)
	}
}

// Enabled used to be dereferenced unchecked.
func TestAuthMethodsWithoutAnEnabledFlagLoadAsDisabled(t *testing.T) {
	logtest.Discard(t)
	enabledAfterLoad := map[Subsystem]func(config.Config) bool{
		SubsystemEmail:  func(c config.Config) bool { return c.Email.Enable },
		SubsystemMobile: func(c config.Config) bool { return c.Mobile.Enable },
		SubsystemDevice: func(c config.Config) bool { return c.Device.Enable },
	}
	for subsystem, isEnabled := range enabledAfterLoad {
		store := healthyStore()
		store.auth.methods[string(subsystem)].Enabled = nil
		deps, h := newHarness(store, config.Config{})

		if err := Reload(context.Background(), deps, subsystem); err != nil {
			t.Fatalf("Reload(%s) = %v", subsystem, err)
		}
		if isEnabled(h.config) {
			t.Fatalf("%s without an enabled flag loaded as enabled", subsystem)
		}
	}
}

// Startup still fails fast, now by returning the first error instead of
// panicking; nothing after the failing subsystem is loaded.
func TestStartupStopsAtTheFirstFailingSubsystem(t *testing.T) {
	logtest.Discard(t)
	store := healthyStore()
	store.system.fail[categoryInvite] = true
	deps, h := newHarness(store, config.Config{})

	err := loadSubsystems(context.Background(), deps, startupOrder)

	if !errors.Is(err, errStoreDown) || !strings.Contains(err.Error(), "invite") {
		t.Fatalf("startup error = %v, want the invite read failure", err)
	}
	if h.config.Site.SiteName == "" || h.config.Node.NodePullInterval == 0 {
		t.Fatalf("subsystems before invite were not loaded: %+v", h.config)
	}
	if h.config.Register.StopRegister || h.config.Subscribe.SubscribePath != "" {
		t.Fatalf("subsystems after the failing one were loaded: %+v", h.config)
	}
}

func TestNodeSecretProvisioning(t *testing.T) {
	logtest.Discard(t)
	t.Run("generates a missing secret", func(t *testing.T) {
		store := healthyStore()
		store.system.settings[categoryNode] = []*system.System{setting(categoryNode, "NodeSecret", "", "string")}
		if err := NodeSecret(context.Background(), store.settingsDeps()); err != nil {
			t.Fatalf("NodeSecret() = %v", err)
		}
		if got := store.system.updates["server.NodeSecret"]; len(got) != nodeSecretLength {
			t.Fatalf("stored secret %q, want %d random characters", got, nodeSecretLength)
		}
	})
	// A stored secret whose row type does not decode is still the secret every
	// node uses; generating a new one would cut them all off.
	t.Run("keeps a secret stored under another type", func(t *testing.T) {
		store := healthyStore()
		store.system.settings[categoryNode] = []*system.System{setting(categoryNode, "NodeSecret", "configured-secret", "bool")}
		if err := NodeSecret(context.Background(), store.settingsDeps()); err != nil {
			t.Fatalf("NodeSecret() = %v", err)
		}
		if len(store.system.updates) != 0 {
			t.Fatalf("stored secret replaced: %v", store.system.updates)
		}
	})
	t.Run("returns store errors instead of panicking", func(t *testing.T) {
		store := healthyStore()
		store.txErr = errStoreDown
		if err := NodeSecret(context.Background(), store.settingsDeps()); !errors.Is(err, errStoreDown) {
			t.Fatalf("NodeSecret() = %v, want the store error", err)
		}
	})
}
