package routes

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
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

// gateStore is adminGuardStore with the database exposed, so a test can
// change an account behind the cache's back.
func gateStore(t *testing.T) (*gorm.DB, *repository.GormStore, *redis.Client) {
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
	return db, store, rds
}

// The account gate request authentication applies reads the row as stored,
// not the cached account: a ban, deletion or demotion written directly to
// the database, as one whose cache invalidation was lost to a Redis hiccup,
// refuses the account's sessions at once, not after the cache's lifetime.
func TestAccountGateSeesChangesTheCacheMissed(t *testing.T) {
	for name, tc := range map[string]struct {
		change   string // SQL applied behind the cache's back
		path     string
		wantCode uint32
	}{
		"ban refuses the session":                {"UPDATE user SET enable = false WHERE id = ?", "/v1/public/user/info", xerr.UserDisabled},
		"deletion refuses the session":           {"UPDATE user SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?", "/v1/public/user/info", xerr.UserNotExist},
		"demotion refuses the administrator":     {"UPDATE user SET is_admin = false WHERE id = ?", "/v1/admin/user/list?page=1&size=10", xerr.InvalidAccess},
		"demotion keeps the member's own routes": {"UPDATE user SET is_admin = false WHERE id = ?", "/v1/public/user/info", 200},
	} {
		t.Run(name, func(t *testing.T) {
			logtest.Discard(t)
			db, store, rds := gateStore(t)
			_, admin := adminGuardUsers(t, store)
			h := server.New()
			RegisterHandlers(h, adminGuardDependencies(store, rds))
			session := adminGuardSession(t, rds, admin)

			// The first request caches the account row and passes.
			w := ut.PerformRequest(h.Engine, http.MethodGet, tc.path, nil, ut.Header{Key: "Authorization", Value: session})
			if code, ok := replyCode(w.Body.Bytes()); !ok || code != 200 {
				t.Fatalf("before the change: %q", w.Body.String())
			}
			if n, _ := rds.Exists(context.Background(), "cache:user:id:"+itoa(admin.Id)).Result(); n != 1 {
				t.Fatal("the account row was not cached")
			}

			// The change lands in the database only; the cache keeps the
			// old row.
			if err := db.Exec(tc.change, admin.Id).Error; err != nil {
				t.Fatal(err)
			}
			if n, _ := rds.Exists(context.Background(), "cache:user:id:"+itoa(admin.Id)).Result(); n != 1 {
				t.Fatal("the cached row was dropped; the test must change the account behind the cache's back")
			}

			w = ut.PerformRequest(h.Engine, http.MethodGet, tc.path, nil, ut.Header{Key: "Authorization", Value: session})
			if code, ok := replyCode(w.Body.Bytes()); !ok || code != tc.wantCode {
				t.Fatalf("after the change: %q, want code %d", w.Body.String(), tc.wantCode)
			}
		})
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
