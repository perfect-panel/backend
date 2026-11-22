package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

const adminGuardJWTKey = "admin-guard-test-only-signing-key"

// Every registered /v1/admin route refuses a signed-in user who is not an
// administrator, however the route was registered: the routes are exercised,
// not their source. An administrator gets through to the handler.
func TestAdminGroupsAreGuarded(t *testing.T) {
	logtest.Discard(t)
	store, rds := adminGuardStore(t)
	member, admin := adminGuardUsers(t, store)
	h := server.New()
	RegisterHandlers(h, adminGuardDependencies(store, rds))

	memberSession := adminGuardSession(t, rds, member)
	var adminRoutes int
	for _, route := range h.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/admin/") {
			continue
		}
		adminRoutes++
		w := ut.PerformRequest(h.Engine, route.Method, concreteRoutePath(route.Path), nil, ut.Header{Key: "Authorization", Value: memberSession})
		if code, ok := replyCode(w.Body.Bytes()); !ok || code != xerr.InvalidAccess {
			t.Errorf("%s %s: a non-administrator got %q, want the refusal with code %d", route.Method, route.Path, w.Body.String(), xerr.InvalidAccess)
		}
	}
	if adminRoutes == 0 {
		t.Fatal("no /v1/admin route is registered")
	}

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/v1/admin/user/list?page=1&size=10", nil, ut.Header{Key: "Authorization", Value: adminGuardSession(t, rds, admin)})
	if code, ok := replyCode(w.Body.Bytes()); !ok || code != 200 || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("an administrator got %q, want the user list served", w.Body.String())
	}
}

// A signed-in user who is not an administrator is refused on an admin route;
// an administrator gets through.
func TestAdminGroupRefusesNonAdministrators(t *testing.T) {
	logtest.Discard(t)
	store, rds := adminGuardStore(t)
	member, admin := adminGuardUsers(t, store)

	h := server.New()
	deps := adminGuardDependencies(store, rds)
	deps.adminGroup(h, "/v1/admin/probe").GET("/", func(_ context.Context, c *app.RequestContext) {
		c.String(200, "served")
	})

	for _, tc := range []struct {
		name       string
		user       *user.User
		wantServed bool
	}{
		{"member", member, false},
		{"administrator", admin, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := ut.PerformRequest(h.Engine, "GET", "/v1/admin/probe/", nil, ut.Header{Key: "Authorization", Value: adminGuardSession(t, rds, tc.user)})
			body := w.Body.String()
			if served := body == "served"; served != tc.wantServed {
				t.Fatalf("served = %v, want %v (body %q)", served, tc.wantServed, body)
			}
			if code, ok := replyCode(w.Body.Bytes()); !tc.wantServed && (!ok || code != xerr.InvalidAccess) {
				t.Fatalf("refusal = %q, want code %d", body, xerr.InvalidAccess)
			}
		})
	}
}

// concreteRoutePath fills a route pattern's parameters so the request
// matches the route.
func concreteRoutePath(pattern string) string {
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
			segments[i] = "1"
		}
	}
	return strings.Join(segments, "/")
}

// replyCode reads the code of a response envelope; ok is false for a body
// that is not one, such as a handler's plain text.
func replyCode(body []byte) (code uint32, ok bool) {
	var reply struct {
		Code uint32 `json:"code"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return 0, false
	}
	return reply.Code, true
}

// adminGuardUsers stores an ordinary member and an administrator.
func adminGuardUsers(t *testing.T, store *repository.GormStore) (member, admin *user.User) {
	t.Helper()
	enabled, isAdmin := true, true
	member = &user.User{Enable: &enabled}
	admin = &user.User{Enable: &enabled, IsAdmin: &isAdmin}
	for _, u := range []*user.User{member, admin} {
		if err := store.User().Insert(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	return member, admin
}

// adminGuardSession signs u in and returns the session token.
func adminGuardSession(t *testing.T, rds *redis.Client, u *user.User) string {
	t.Helper()
	signed, err := usersession.Issue(context.Background(), rds, adminGuardJWTKey, 3600, usersession.Grant{UserID: u.Id})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// adminGuardDependencies wires the routes onto the identity facade over
// store, which resolves the sessions and serves the user list; the other
// facades stay nil, and the guard keeps every non-administrator away from
// their handlers.
func adminGuardDependencies(store *repository.GormStore, rds *redis.Client) Dependencies {
	accounts := identity.New(identity.Deps{
		Store: store, Redis: rds, Users: store.User(), UserAuths: store.UserAuth(), Devices: store.UserDevice(),
		Cache: store.UserCache(), Logs: store.Log(), Auths: store.Auth(), Wallet: noWallets{},
	})
	return Dependencies{
		Config:   config.Config{Boot: config.Boot{JwtAuth: config.JwtAuth{AccessSecret: adminGuardJWTKey, AccessExpire: 3600}}},
		Redis:    rds,
		Identity: accounts,
	}
}

// noWallets is the billing wallet port of a database without wallet rows.
type noWallets struct{}

var _ identity.Wallets = noWallets{}

func (noWallets) FindWallet(context.Context, int64) (*wallet.Wallet, error) { return nil, nil }
func (noWallets) FindWallets(context.Context, []int64) (map[int64]*wallet.Wallet, error) {
	return map[int64]*wallet.Wallet{}, nil
}
func (noWallets) OpenWallet(context.Context, wallet.Wallet) error       { return nil }
func (noWallets) AdjustWallet(context.Context, wallet.Adjustment) error { return nil }

func adminGuardStore(t *testing.T) (*repository.GormStore, *redis.Client) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true, IgnoreRelationshipsWhenMigrating: true, Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&user.User{}, &user.AuthMethods{}); err != nil {
		t.Fatal(err)
	}
	// SQLite index names are database-global, unlike the MySQL entity tags.
	if err := db.Migrator().RenameIndex(&user.AuthMethods{}, "idx_user_id", "idx_auth_methods_user_id"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&user.Device{}); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rds.Close() })
	store := repository.NewGormStoreWithBuilders(db, rds, repository.Builders{
		Identity: identity.NewRepoBuilder(), Platform: platform.NewRepoBuilder(),
		Billing: func(repository.ModuleConn) repository.BillingRepos { return repository.BillingRepos{} },
		Network: func(repository.ModuleConn) repository.NetworkRepos { return repository.NetworkRepos{} },
		Subscription: func(repository.ModuleConn, repository.NodeCacheKeyBridge) repository.SubscriptionRepos {
			return repository.SubscriptionRepos{}
		},
		Support:      func(repository.ModuleConn) repository.SupportRepos { return repository.SupportRepos{} },
		Notification: func(repository.ModuleConn) repository.NotificationRepos { return repository.NotificationRepos{} },
	})
	return store, rds
}
