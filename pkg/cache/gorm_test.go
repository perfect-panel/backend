package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/plugin/soft_delete"
)

func TestInvalidationQueueDefersDeletionUntilFlush(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.Set(ctx, "user:42", "stale", 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	queue := NewInvalidationQueue()
	conn := NewConn(nil, client, WithInvalidationQueue(queue))
	if err := conn.DelCacheCtx(ctx, "user:42", "user:42"); err != nil {
		t.Fatalf("queue invalidation: %v", err)
	}
	if !server.Exists("user:42") {
		t.Fatal("cache key was deleted before transaction commit")
	}
	if err := queue.Flush(ctx, client); err != nil {
		t.Fatalf("flush invalidations: %v", err)
	}
	if server.Exists("user:42") {
		t.Fatal("cache key was not deleted after transaction commit")
	}
}

func TestInvalidationQueueRetainsKeysAfterFlushFailure(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	if err := client.Set(ctx, "user:42", "stale", 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	queue := NewInvalidationQueue()
	queue.Add("user:42")
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
	if err := queue.Flush(ctx, client); err == nil {
		t.Fatal("expected flush to fail with a closed redis client")
	}

	retryClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = retryClient.Close() })
	if err := queue.Flush(ctx, retryClient); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if server.Exists("user:42") {
		t.Fatal("retry did not invalidate queued cache key")
	}
}

func TestInvalidationRetrierKeepsConcurrentInvalidation(t *testing.T) {
	retrier := &InvalidationRetrier{keys: map[string]uint64{"user:42": 1}, sequence: 1}
	pending := retrier.snapshot()
	retrier.sequence++
	retrier.keys["user:42"] = retrier.sequence
	retrier.remove(pending)
	if _, ok := retrier.snapshot()["user:42"]; !ok {
		t.Fatal("concurrent invalidation was removed by an older successful retry")
	}
}

func TestQueryCtxBypassesCacheInsideTransaction(t *testing.T) {
	db := newDryRunDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.Set(ctx, "user:42", `{"id":42,"email":"old@example.com"}`, 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	type cachedUser struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	value := cachedUser{}
	queryCalled := false
	conn := NewConn(db, client, WithInvalidationQueue(NewInvalidationQueue()))
	if err := conn.QueryCtx(ctx, &value, "user:42", func(_ *gorm.DB, v interface{}) error {
		queryCalled = true
		*v.(*cachedUser) = cachedUser{ID: 42, Email: "new@example.com"}
		return nil
	}); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !queryCalled || value.Email != "new@example.com" {
		t.Fatalf("transaction read used stale cache: called=%v value=%+v", queryCalled, value)
	}
}

func TestVersionFenceRejectsStaleCacheFill(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	conn := NewConn(nil, client)
	version, err := conn.cacheVersion(ctx, "user:42")
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if err := conn.invalidateCacheKeys(ctx, "user:42"); err != nil {
		t.Fatalf("invalidate cache: %v", err)
	}
	if err := conn.setCacheIfVersion(ctx, "user:42", version, map[string]string{"email": "old@example.com"}); err != nil {
		t.Fatalf("set cache with stale version: %v", err)
	}
	if server.Exists("user:42") {
		t.Fatal("stale read repopulated cache after invalidation")
	}
}

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

func TestExecCtxDoesNotReportCacheDeletionFailureAsWriteFailure(t *testing.T) {
	db := newDryRunDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}

	err := NewConn(db, client).ExecCtx(context.Background(), func(*gorm.DB) error {
		return nil
	}, "cache:key")
	if err != nil {
		t.Fatalf("a successful database write must not fail because cache deletion failed: %v", err)
	}
}

type User struct {
	Id                    int64                 `gorm:"primarykey"`
	Email                 string                `gorm:"index:idx_email;type:varchar(100);unique;not null;comment:电子邮箱"`
	Password              string                `gorm:"type:varchar(100);comment:用户密码;not null"`
	Avatar                string                `gorm:"type:varchar(200);default:'';comment:用户头像"`
	Balance               int64                 `gorm:"default:0;comment:用户余额"`
	Telegram              int64                 `gorm:"default:null;comment:Telegram账号"`
	ReferCode             string                `gorm:"type:varchar(20);default:'';comment:推荐码"`
	RefererId             int64                 `gorm:"comment:推荐人ID"`
	Enable                bool                  `gorm:"default:true;not null;comment:账户是否可用"`
	IsAdmin               bool                  `gorm:"default:false;not null;comment:是否管理员"`
	ValidEmail            bool                  `gorm:"default:false;not null;comment:是否验证邮箱"`
	EnableEmailNotify     bool                  `gorm:"default:false;not null;comment:是否启用邮件通知"`
	EnableTelegramNotify  bool                  `gorm:"default:false;not null;comment:是否启用Telegram通知"`
	EnableBalanceNotify   bool                  `gorm:"default:false;not null;comment:是否启用余额变动通知"`
	EnableLoginNotify     bool                  `gorm:"default:false;not null;comment:是否启用登录通知"`
	EnableSubscribeNotify bool                  `gorm:"default:false;not null;comment:是否启用订阅通知"`
	EnableTradeNotify     bool                  `gorm:"default:false;not null;comment:是否启用交易通知"`
	CreatedAt             time.Time             `gorm:"<-:create;comment:创建时间"`
	UpdatedAt             time.Time             `gorm:"comment:更新时间"`
	DeletedAt             gorm.DeletedAt        `gorm:"default:null;comment:删除时间"`
	IsDel                 soft_delete.DeletedAt `gorm:"softDelete:flag,DeletedAtField:DeletedAt;comment:1:正常 0:删除"` // Use `1` `0` to identify
}

func TestGormCacheCtx(t *testing.T) {
	t.Skipf("skip TestGormCacheCtx test")
	db, err := orm.ConnectMysql(orm.Mysql{
		Config: orm.Config{
			Addr:     "localhost:3306",
			Config:   "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai",
			Dbname:   "vpnboard",
			Username: "root",
			Password: "mylove520",
		},
	})
	if err != nil {
		t.Error(err)
	}
	rds := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	conn := NewConn(db, rds)
	var u User
	key := "user:id"
	err = conn.QueryCtx(context.Background(), &u, key, func(conn *gorm.DB, v interface{}) error {
		return conn.Where("id = ?", 1).First(v).Error
	})
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("get cache success %+v", u)
}
