// Package identitytest runs the identity module's real repositories over an
// in-memory SQLite database and miniredis, for behaviour tests that check
// what a flow wrote rather than which dependency it called.
package identitytest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/repo"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// ClientIP and UserAgent are the request metadata Context carries.
const (
	ClientIP  = "203.0.113.7"
	UserAgent = "identity-test/1.0"
)

// Env is a store over a fresh database and Redis.
type Env struct {
	DB    *gorm.DB
	Store *repository.GormStore
	Redis *redis.Client
	Mini  *miniredis.Miniredis
}

var databases atomic.Int64

// New creates the identity tables, the device online records, the audit log
// and the event outbox.
func New(t testing.TB) *Env {
	t.Helper()
	name := fmt.Sprintf("file:identitytest-%d?mode=memory&cache=shared", databases.Add(1))
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{
		TranslateError:                   true,
		IgnoreRelationshipsWhenMigrating: true,
		Logger:                           gormlog.Default.LogMode(gormlog.Silent),
	})
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
	// SQLite index names are database-wide, unlike the MySQL entity tags.
	if err := db.Migrator().RenameIndex(&user.AuthMethods{}, "idx_user_id", "idx_auth_methods_user_id"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&user.Device{}, &user.DeviceOnlineRecord{}, &auth.Auth{}, &log.SystemLog{}, &outbox.Event{}, &inbox.Record{}); err != nil {
		t.Fatal(err)
	}

	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rds.Close() })
	// One retrier per store, as the module's builder creates it, so failed
	// account-cache invalidations are redone as in production.
	retrier := cache.NewInvalidationRetrier(rds)
	store := repository.NewGormStoreWithBuilders(db, rds, repository.Builders{
		Identity: func(c repository.ModuleConn, bridges repository.IdentityBridges) repository.IdentityRepos {
			users := repo.NewUserRepo(c.Conn(), bridges, repo.WithInvalidationRetrier(retrier))
			return repository.IdentityRepos{Users: users, UserAuths: users, Devices: users, UserCache: users, Auths: repo.NewAuthRepo(c.Conn())}
		},
		Platform: platform.NewRepoBuilder(),
		Billing:  func(repository.ModuleConn) repository.BillingRepos { return repository.BillingRepos{} },
		Network:  func(repository.ModuleConn) repository.NetworkRepos { return repository.NetworkRepos{} },
		Subscription: func(repository.ModuleConn, repository.NodeCacheKeyBridge) repository.SubscriptionRepos {
			return repository.SubscriptionRepos{CacheBridge: noSubscriptions{}, ScopeBridge: noSubscriptions{}}
		},
		Support:      func(repository.ModuleConn) repository.SupportRepos { return repository.SupportRepos{} },
		Notification: func(repository.ModuleConn) repository.NotificationRepos { return repository.NotificationRepos{} },
	})
	return &Env{DB: db, Store: store, Redis: rds, Mini: mini}
}

// Context is a request context carrying ClientIP and UserAgent, as the
// access-log middleware sets them for every HTTP request.
func Context() context.Context {
	return requestmeta.With(context.Background(), requestmeta.New(ClientIP, UserAgent))
}

// EnableMethod stores the configuration of an authentication method.
func (e *Env) EnableMethod(t testing.TB, method, config string) {
	t.Helper()
	enabled := true
	if err := e.DB.Create(&auth.Auth{Method: method, Config: config, Enabled: &enabled}).Error; err != nil {
		t.Fatal(err)
	}
}

// Logs returns the audit rows of type typ about the object id.
func (e *Env) Logs(t testing.TB, typ log.Type, objectID int64) []log.SystemLog {
	t.Helper()
	var rows []log.SystemLog
	if err := e.DB.Where("type = ? AND object_id = ?", typ.Uint8(), objectID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// Events returns the outbox events of topic.
func (e *Env) Events(t testing.TB, topic string) []outbox.Event {
	t.Helper()
	var events []outbox.Event
	if err := e.DB.Where("topic = ?", topic).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	return events
}

// Identities returns the bindings of the account userID.
func (e *Env) Identities(t testing.TB, userID int64) []user.AuthMethods {
	t.Helper()
	var methods []user.AuthMethods
	if err := e.DB.Where("user_id = ?", userID).Order("id").Find(&methods).Error; err != nil {
		t.Fatal(err)
	}
	return methods
}

// Users returns every account row, deleted ones included.
func (e *Env) Users(t testing.TB) []user.User {
	t.Helper()
	var users []user.User
	if err := e.DB.Unscoped().Order("id").Find(&users).Error; err != nil {
		t.Fatal(err)
	}
	return users
}

// noSubscriptions is the subscription side of a store without
// subscriptions.
type noSubscriptions struct{}

func (noSubscriptions) QueryUserSubscribe(context.Context, int64, ...int64) ([]*usersub.SubscribeDetails, error) {
	return nil, nil
}
func (noSubscriptions) ClearSubscribeCache(context.Context, ...*usersub.Subscribe) error { return nil }
func (noSubscriptions) UpdateUserSubscribeCache(context.Context, *usersub.Subscribe) error {
	return nil
}
func (noSubscriptions) SubscriptionUserIDs(context.Context, repository.SubscriptionUserFilter) ([]int64, error) {
	return nil, nil
}
