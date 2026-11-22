package systemsetting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// platformTx runs the settings writes in a real transaction over the test
// database; failCommit makes the transaction roll back after the writes.
type platformTx struct {
	db         *gorm.DB
	rds        *redis.Client
	failCommit error
}

var _ SettingsTransactor = (*platformTx)(nil)

func (p *platformTx) InSettingsTx(ctx context.Context, fn func(SettingsStore) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fn(settingsStore{SettingsWriter: repo.NewSystemRepo(cache.NewConn(tx, p.rds)), AuditWriter: repo.NewLogRepo(tx)}); err != nil {
			return err
		}
		return p.failCommit
	})
}

// runtime records what the service asks of the running process.
type runtime struct {
	mu            sync.Mutex
	reinitialized []string
	reloadErr     error
	restarts      chan struct{}
	restartErr    error
	path          string
}

func (r *runtime) reinitialize(subsystem string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reinitialized = append(r.reinitialized, subsystem)
	return r.reloadErr
}

func (r *runtime) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reinitialized)
}

func (r *runtime) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reinitialized = nil
}

type settingsWorld struct {
	svc     *Service
	db      *gorm.DB
	system  kernel.SystemRepo
	tx      *platformTx
	runtime *runtime
}

func newSettingsWorld(t *testing.T) *settingsWorld {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:settings-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&system.System{}, &log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })

	w := &settingsWorld{
		db:      db,
		system:  repo.NewSystemRepo(cache.NewConn(db, rds)),
		tx:      &platformTx{db: db, rds: rds},
		runtime: &runtime{restarts: make(chan struct{}, 1), path: "/sub"},
	}
	w.svc = NewService(Deps{
		System:       w.system,
		Store:        w.tx,
		Reinitialize: w.runtime.reinitialize,
		Restart: func() error {
			w.runtime.restarts <- struct{}{}
			return w.runtime.restartErr
		},
		SubscribePath: func() string { return w.runtime.path },
		Multiplier:    func(time.Time) float32 { return 1.5 },
	})
	// The rows the migrations seed with a fixed type.
	for key, value := range map[string]string{"DNS": "", "Block": "", "Outbound": "", "NodeMultiplierConfig": "[]"} {
		if err := w.system.UpdateValueByCategoryKey(context.Background(), "server", key, value, "string"); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// Every setting reads back as it was written, except the secrets, which read
// back masked (see TestSecretsReadBackMaskedAndAreKeptWhenMasked).
func TestSettingsReadBackAsWritten(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := context.Background()
	check := func(name string, update func() error, read func() (any, error), want any) {
		t.Helper()
		if err := update(); err != nil {
			t.Fatalf("%s: update: %v", name, err)
		}
		got, err := read()
		if err != nil {
			t.Fatalf("%s: read: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s reads back as\n%+v\nwant\n%+v", name, got, want)
		}
	}

	site := &dto.SiteConfig{Host: "panel.example", SiteName: "Panel", SiteDesc: "desc", SiteLogo: "/logo.png", Keywords: "vpn", CustomHTML: "<b>hi</b>", CustomData: `{"a":1}`}
	check("site", func() error { return w.svc.UpdateSiteConfig(ctx, site) },
		func() (any, error) { return w.svc.GetSiteConfig(ctx) }, site)

	subscribe := &dto.SubscribeConfig{SingleModel: true, SubscribePath: "/sub", SubscribeDomain: "sub.example", PanDomain: true, UserAgentLimit: true, UserAgentList: "clash", ShowTutorial: true, ProfileUpdateInterval: 12, ProfileWebPageURL: "https://panel.example"}
	check("subscribe", func() error { return w.svc.UpdateSubscribeConfig(ctx, subscribe) },
		func() (any, error) { return w.svc.GetSubscribeConfig(ctx) }, subscribe)

	register := &dto.RegisterConfig{StopRegister: true, EnableTrial: true, TrialSubscribe: 3, TrialTime: 7, TrialTimeUnit: "Day", EnableIpRegisterLimit: true, IpRegisterLimit: 5, IpRegisterLimitDuration: 60}
	check("register", func() error { return w.svc.UpdateRegisterConfig(ctx, register) },
		func() (any, error) { return w.svc.GetRegisterConfig(ctx) }, register)

	verify := &dto.VerifyConfig{TurnstileSiteKey: "site-key", TurnstileSecret: "secret", EnableLoginVerify: true, EnableRegisterVerify: true, EnableResetPasswordVerify: true}
	maskedVerify := *verify
	maskedVerify.TurnstileSecret = dto.SecretMask
	check("verify", func() error { return w.svc.UpdateVerifyConfig(ctx, verify) },
		func() (any, error) { return w.svc.GetVerifyConfig(ctx) }, &maskedVerify)

	invite := &dto.InviteConfig{ForcedInvite: true, ReferralPercentage: 20, OnlyFirstPurchase: true, WithdrawalMethod: "usdt"}
	check("invite", func() error { return w.svc.UpdateInviteConfig(ctx, invite) },
		func() (any, error) { return w.svc.GetInviteConfig(ctx) }, invite)

	tos := &dto.TosConfig{TosContent: "terms"}
	check("tos", func() error { return w.svc.UpdateTosConfig(ctx, tos) },
		func() (any, error) { return w.svc.GetTosConfig(ctx) }, tos)

	privacy := &dto.PrivacyPolicyConfig{PrivacyPolicy: "privacy"}
	check("privacy policy", func() error { return w.svc.UpdatePrivacyPolicyConfig(ctx, privacy) },
		func() (any, error) { return w.svc.GetPrivacyPolicyConfig(ctx) }, privacy)
	// Both live in the tos category without overwriting each other.
	if got, err := w.svc.GetTosConfig(ctx); err != nil || got.TosContent != "terms" {
		t.Fatalf("tos after the privacy policy update = %+v (err %v)", got, err)
	}

	currency := &dto.CurrencyConfig{AccessKey: "key", CurrencyUnit: "USD", CurrencySymbol: "$"}
	check("currency", func() error { return w.svc.UpdateCurrencyConfig(ctx, currency) },
		func() (any, error) { return w.svc.GetCurrencyConfig(ctx) }, &dto.CurrencyConfig{AccessKey: dto.SecretMask, CurrencyUnit: "USD", CurrencySymbol: "$"})

	verifyCode := &dto.VerifyCodeConfig{VerifyCodeExpireTime: 300, VerifyCodeLimit: 15, VerifyCodeInterval: 60}
	check("verify code", func() error { return w.svc.UpdateVerifyCodeConfig(ctx, verifyCode) },
		func() (any, error) { return w.svc.GetVerifyCodeConfig(ctx) }, verifyCode)

	node := &dto.NodeConfig{
		NodeSecret: "secret", NodePullInterval: 10, NodePushInterval: 60, TrafficReportThreshold: 1024, IPStrategy: "prefer_ipv4",
		DNS:      []dto.PlatformNodeDNSSnapshot{{Proto: "udp", Address: "1.1.1.1:53", Domains: []string{"example.com"}}},
		Block:    []string{"ads.example"},
		Outbound: []dto.PlatformNodeOutboundSnapshot{{Name: "warp", Protocol: "wireguard", Address: "162.159.192.1", Port: 2408, Password: "k", Rules: []string{"geosite:openai"}}},
	}
	// The node secret reads back in clear: administrators copy it to deploy
	// nodes. The outbound credentials read back masked.
	maskedNode := *node
	maskedNode.Outbound = []dto.PlatformNodeOutboundSnapshot{{Name: "warp", Protocol: "wireguard", Address: "162.159.192.1", Port: 2408, Password: dto.SecretMask, Rules: []string{"geosite:openai"}}}
	check("node", func() error { return w.svc.UpdateNodeConfig(ctx, node) },
		func() (any, error) { return w.svc.GetNodeConfig(ctx) }, &maskedNode)

	periods := []dto.TimePeriod{{StartTime: "00:00", EndTime: "06:00", Multiplier: 0.5}}
	check("node multiplier", func() error { return w.svc.SetNodeMultiplier(ctx, &dto.SetNodeMultiplierRequest{Periods: periods}) },
		func() (any, error) { return w.svc.GetNodeMultiplier(ctx) }, &dto.GetNodeMultiplierResponse{Periods: periods})
}

// Each update re-initializes the subsystem that owns the settings, after the
// write committed.
func TestUpdatesReinitializeTheirSubsystem(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		update func() error
		want   []string
	}{
		{"site", func() error { return w.svc.UpdateSiteConfig(ctx, &dto.SiteConfig{SiteName: "x"}) }, []string{"site"}},
		{"subscribe", func() error { return w.svc.UpdateSubscribeConfig(ctx, &dto.SubscribeConfig{SubscribePath: "/sub"}) }, []string{"subscribe"}},
		{"register", func() error { return w.svc.UpdateRegisterConfig(ctx, &dto.RegisterConfig{}) }, []string{"register"}},
		{"verify", func() error { return w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{}) }, []string{"verify"}},
		{"verify code", func() error { return w.svc.UpdateVerifyCodeConfig(ctx, &dto.VerifyCodeConfig{}) }, []string{"verify"}},
		{"invite", func() error { return w.svc.UpdateInviteConfig(ctx, &dto.InviteConfig{}) }, []string{"invite"}},
		{"currency", func() error { return w.svc.UpdateCurrencyConfig(ctx, &dto.CurrencyConfig{}) }, []string{"currency"}},
		{"node", func() error { return w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{}) }, []string{"node"}},
		{"node multiplier", func() error { return w.svc.SetNodeMultiplier(ctx, &dto.SetNodeMultiplierRequest{}) }, []string{"node"}},
		{"telegram", func() error { return w.svc.SettingTelegramBot(ctx) }, []string{"telegram"}},
		{"tos", func() error { return w.svc.UpdateTosConfig(ctx, &dto.TosConfig{}) }, nil},
		{"privacy policy", func() error { return w.svc.UpdatePrivacyPolicyConfig(ctx, &dto.PrivacyPolicyConfig{}) }, nil},
	} {
		w.runtime.reset()
		if err := tc.update(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := w.runtime.calls(); !slices.Equal(got, tc.want) {
			t.Fatalf("%s re-initialized %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A reload that fails reaches the administrator under a code of its own: the
// settings are stored, but the running server still uses the old ones, which
// an internal error would hide.
func TestUpdateReportsAFailedReload(t *testing.T) {
	w := newSettingsWorld(t)
	w.runtime.reloadErr = errors.New("reload failed")
	err := w.svc.UpdateSiteConfig(context.Background(), &dto.SiteConfig{SiteName: "x"})
	if !errors.Is(err, w.runtime.reloadErr) || xerr.CodeOf(err) != xerr.SettingsSavedNotApplied {
		t.Fatalf("update = %v, want the reload failure under SettingsSavedNotApplied", err)
	}
	if got := w.runtime.calls(); !slices.Equal(got, []string{"site"}) {
		t.Fatalf("re-initialized %v, want [site]", got)
	}
	if got, err := w.svc.GetSiteConfig(context.Background()); err != nil || got.SiteName != "x" {
		t.Fatalf("site after the failed reload = %+v (err %v), want the saved settings", got, err)
	}
	// The admin panel reads the code and its message, not an internal error.
	want := httpx.HTTPResult{StatusCode: http.StatusOK, Body: httpx.Error(xerr.SettingsSavedNotApplied,
		"Settings saved but could not be applied; reload or restart the service")}
	if got := httpx.BuildHTTPResult(nil, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("http result = %+v, want %+v", got, want)
	}
}

// Reading the verification settings is a read: it used to re-initialize the
// verify subsystem on every admin page load.
func TestGetVerifyConfigOnlyReads(t *testing.T) {
	w := newSettingsWorld(t)
	if _, err := w.svc.GetVerifyConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := w.runtime.calls(); len(calls) != 0 {
		t.Fatalf("GetVerifyConfig re-initialized %v", calls)
	}
}

// A new subscribe path needs the HTTP server rebuilt: the service restarts it
// in the background instead of re-initializing.
func TestUpdateSubscribeConfigRestartsForANewPath(t *testing.T) {
	w := newSettingsWorld(t)
	w.runtime.restartErr = errors.New("restart failed") // logged, not returned
	if err := w.svc.UpdateSubscribeConfig(context.Background(), &dto.SubscribeConfig{SubscribePath: "/new"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.runtime.restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("the server was not restarted")
	}
	if calls := w.runtime.calls(); len(calls) != 0 {
		t.Fatalf("re-initialized %v instead of restarting only", calls)
	}
	if got, err := w.svc.GetSubscribeConfig(context.Background()); err != nil || got.SubscribePath != "/new" {
		t.Fatalf("stored path = %+v (err %v)", got, err)
	}
}

// A failed transaction leaves the settings untouched and the running
// configuration alone.
func TestFailedUpdatesChangeNothing(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := context.Background()
	if err := w.svc.UpdateSiteConfig(ctx, &dto.SiteConfig{SiteName: "before"}); err != nil {
		t.Fatal(err)
	}
	w.runtime.reset()
	w.tx.failCommit = errors.New("commit failed")
	for name, update := range map[string]func() error{
		"site":        func() error { return w.svc.UpdateSiteConfig(ctx, &dto.SiteConfig{SiteName: "after"}) },
		"verify":      func() error { return w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "after"}) },
		"node":        func() error { return w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{NodeSecret: "after"}) },
		"subscribe":   func() error { return w.svc.UpdateSubscribeConfig(ctx, &dto.SubscribeConfig{SubscribePath: "/after"}) },
		"register":    func() error { return w.svc.UpdateRegisterConfig(ctx, &dto.RegisterConfig{TrialTimeUnit: "after"}) },
		"invite":      func() error { return w.svc.UpdateInviteConfig(ctx, &dto.InviteConfig{WithdrawalMethod: "after"}) },
		"tos":         func() error { return w.svc.UpdateTosConfig(ctx, &dto.TosConfig{TosContent: "after"}) },
		"verify code": func() error { return w.svc.UpdateVerifyCodeConfig(ctx, &dto.VerifyCodeConfig{VerifyCodeLimit: 1}) },
	} {
		if err := update(); xerr.CodeOf(err) != xerr.DatabaseUpdateError {
			t.Fatalf("%s: err = %v, want a database update error", name, err)
		}
	}
	if calls := w.runtime.calls(); len(calls) != 0 {
		t.Fatalf("failed updates touched the runtime: %v", calls)
	}
	if got, err := w.svc.GetSiteConfig(ctx); err != nil || got.SiteName != "before" {
		t.Fatalf("site after a failed update = %+v (err %v)", got, err)
	}
	if got, err := w.svc.GetVerifyConfig(ctx); err != nil || got.TurnstileSiteKey != "" {
		t.Fatalf("verify after a failed update = %+v (err %v)", got, err)
	}
	select {
	case <-w.runtime.restarts:
		t.Fatal("a failed subscribe update restarted the server")
	default:
	}
}

// A malformed node document is an error for the admin console, not a panic.
func TestGetNodeConfigReportsAMalformedDocument(t *testing.T) {
	for _, key := range []string{"DNS", "Outbound"} {
		w := newSettingsWorld(t)
		if err := w.system.UpdateValueByCategoryKey(context.Background(), "server", key, "{not json"); err != nil {
			t.Fatal(err)
		}
		got, err := w.svc.GetNodeConfig(context.Background())
		if err == nil || got != nil || xerr.CodeOf(err) != xerr.ERROR {
			t.Fatalf("%s: GetNodeConfig = %+v, %v; want an error", key, got, err)
		}
	}
}

func TestNodeMultiplier(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := context.Background()
	empty, err := w.svc.GetNodeMultiplier(ctx)
	if err != nil || len(empty.Periods) != 0 {
		t.Fatalf("initial multiplier = %+v (err %v)", empty, err)
	}
	if err := w.system.UpdateNodeMultiplierConfig(ctx, "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.GetNodeMultiplier(ctx); xerr.CodeOf(err) != xerr.ERROR {
		t.Fatalf("malformed multiplier: err = %v, want an error", err)
	}

	preview, err := w.svc.PreViewNodeMultiplier(ctx)
	if err != nil || preview.Ratio != 1.5 {
		t.Fatalf("preview = %+v (err %v), want the current multiplier", preview, err)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", preview.CurrentTime); err != nil {
		t.Fatalf("preview time %q: %v", preview.CurrentTime, err)
	}
}

func TestSettingsReadFailures(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := context.Background()
	if err := w.db.Migrator().DropTable(&system.System{}); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() error{
		"site":            func() error { _, err := w.svc.GetSiteConfig(ctx); return err },
		"subscribe":       func() error { _, err := w.svc.GetSubscribeConfig(ctx); return err },
		"register":        func() error { _, err := w.svc.GetRegisterConfig(ctx); return err },
		"verify":          func() error { _, err := w.svc.GetVerifyConfig(ctx); return err },
		"verify code":     func() error { _, err := w.svc.GetVerifyCodeConfig(ctx); return err },
		"invite":          func() error { _, err := w.svc.GetInviteConfig(ctx); return err },
		"tos":             func() error { _, err := w.svc.GetTosConfig(ctx); return err },
		"privacy policy":  func() error { _, err := w.svc.GetPrivacyPolicyConfig(ctx); return err },
		"currency":        func() error { _, err := w.svc.GetCurrencyConfig(ctx); return err },
		"node":            func() error { _, err := w.svc.GetNodeConfig(ctx); return err },
		"node multiplier": func() error { _, err := w.svc.GetNodeMultiplier(ctx); return err },
	} {
		if err := read(); xerr.CodeOf(err) != xerr.DatabaseQueryError {
			t.Fatalf("%s: err = %v, want a database query error", name, err)
		}
	}
}

// platformStore is the whole platform store over one test transaction.
type platformStore struct {
	tx  *gorm.DB
	rds *redis.Client
}

var _ kernel.PlatformStore = platformStore{}

func (s platformStore) System() kernel.SystemRepo {
	return repo.NewSystemRepo(cache.NewConn(s.tx, s.rds))
}
func (s platformStore) Task() kernel.TaskRepo     { return repo.NewTaskRepo(s.tx) }
func (s platformStore) Log() kernel.LogRepo       { return repo.NewLogRepo(s.tx) }
func (s platformStore) Inbox() kernel.InboxRepo   { return repo.NewInboxRepo(s.tx) }
func (s platformStore) Outbox() kernel.OutboxRepo { return repo.NewOutboxRepo(s.tx) }

// storeTx runs platform-scoped transactions on the test database, as the
// application store does.
type storeTx struct {
	db  *gorm.DB
	rds *redis.Client
}

var _ PlatformTransactor = storeTx{}

func (p storeTx) InPlatformTx(ctx context.Context, fn func(kernel.PlatformStore) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(platformStore{tx: tx, rds: p.rds})
	})
}

// The facade's transactor writes the settings into the system settings of
// a platform transaction: they commit with it, or roll back with it.
func TestSettingsTransactorWritesInAPlatformTransaction(t *testing.T) {
	w := newSettingsWorld(t)
	settings := NewSettingsTransactor(storeTx{db: w.db, rds: w.tx.rds})
	ctx := context.Background()
	write := func(value string, then error) error {
		return settings.InSettingsTx(ctx, func(writer SettingsStore) error {
			if err := writer.UpdateValueByCategoryKey(ctx, "site", "SiteName", value, "string"); err != nil {
				return err
			}
			return then
		})
	}
	if err := write("committed", nil); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("abort")
	if err := write("rolled back", failure); !errors.Is(err, failure) {
		t.Fatalf("write = %v, want the failure", err)
	}
	if got, err := w.svc.GetSiteConfig(ctx); err != nil || got.SiteName != "committed" {
		t.Fatalf("site = %+v (err %v), want the committed name", got, err)
	}
}
