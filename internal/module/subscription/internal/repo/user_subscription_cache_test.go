package repo

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func subscribeDetailIDs(items []*usersub.SubscribeDetails) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Id)
	}
	return ids
}

func TestQueryUserSubscribeFiltersSharedCachedList(t *testing.T) {
	ctx := context.Background()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	repo := NewUserSubscriptionRepo(repository.ModuleConn{Redis: redisClient}.Conn())
	cacheKey := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, 42)
	cached := []*usersub.SubscribeDetails{
		{Id: 3, UserId: 42, Status: 1},
		{Id: 2, UserId: 42, Status: 4},
		{Id: 1, UserId: 42, Status: 5},
	}
	if err := repo.SetCacheCtx(ctx, cacheKey, cached); err != nil {
		t.Fatalf("seed subscription cache: %v", err)
	}

	dashboard, err := repo.QueryUserSubscribe(ctx, 42, 0, 1, 2, 3)
	if err != nil {
		t.Fatalf("dashboard QueryUserSubscribe: %v", err)
	}
	if got := subscribeDetailIDs(dashboard); fmt.Sprint(got) != "[3]" {
		t.Fatalf("dashboard subscriptions = %v, want [3]", got)
	}

	admin, err := repo.QueryUserSubscribe(ctx, 42, 0, 1, 2, 3, 4, 5)
	if err != nil {
		t.Fatalf("admin QueryUserSubscribe: %v", err)
	}
	if got := subscribeDetailIDs(admin); fmt.Sprint(got) != "[3 2 1]" {
		t.Fatalf("admin subscriptions = %v, want [3 2 1]", got)
	}

	unfiltered, err := repo.QueryUserSubscribe(ctx, 42)
	if err != nil {
		t.Fatalf("unfiltered QueryUserSubscribe: %v", err)
	}
	if got := subscribeDetailIDs(unfiltered); fmt.Sprint(got) != "[3 2 1]" {
		t.Fatalf("unfiltered subscriptions = %v, want [3 2 1]", got)
	}
}

func TestQueryUserSubscribeCachesUnfilteredListWithStableOrder(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
				Logger:                 gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
			})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}
			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = redisClient.Close() })

			if _, err := NewUserSubscriptionRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()).QueryUserSubscribe(context.Background(), 42, 0, 1, 2, 3); err != nil {
				t.Fatalf("QueryUserSubscribe: %v", err)
			}

			sql := logs.String()
			if strings.Contains(sql, "status IN") {
				t.Fatalf("cached query must load the complete list before filtering:\n%s", sql)
			}
			for _, unwanted := range []string{"expire_time >", "finished_at >=", "expire_time ="} {
				if strings.Contains(sql, unwanted) {
					t.Fatalf("subscription history query must not apply a retention window %q:\n%s", unwanted, sql)
				}
			}
			for _, want := range []string{
				"ORDER BY CASE WHEN status = 1 THEN 0 ELSE 1 END ASC",
				"expire_time DESC",
				"id DESC",
			} {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
		})
	}
}

func TestSubscribeRepoInvalidatesEveryCachedUserListThatCanContainPlan(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
				Logger:                 gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
			})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}
			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = redisClient.Close() })

			if _, err := NewSubscribeRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn(), nil).(*subscribeRepo).getUserSubscribeCacheKeys(context.Background(), 10); err != nil {
				t.Fatalf("getUserSubscribeCacheKeys: %v", err)
			}

			sql := logs.String()
			if strings.Contains(sql, "status IN") {
				t.Fatalf("cache invalidation must not filter subscriptions by status:\n%s", sql)
			}
			for _, unwanted := range []string{"expire_time >", "finished_at >=", "expire_time ="} {
				if strings.Contains(sql, unwanted) {
					t.Fatalf("cache invalidation must include historical subscriptions, found %q:\n%s", unwanted, sql)
				}
			}
			for _, want := range []string{"subscribe_id = 10"} {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
		})
	}
}

func TestSubscriptionPolicyQueriesExcludeDeductedSubscriptions(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
				Logger:                 gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
			})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}
			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = redisClient.Close() })

			repo := NewUserSubscriptionRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn())
			if _, err := repo.CountQuotaConsumingSubscriptions(context.Background(), 42, 10); err != nil {
				t.Fatalf("CountQuotaConsumingSubscriptions: %v", err)
			}
			if _, err := repo.HasBlockingSubscription(context.Background(), 42); err != nil {
				t.Fatalf("HasBlockingSubscription: %v", err)
			}

			sql := logs.String()
			if strings.Count(sql, "status <> 4") != 2 {
				t.Fatalf("expected both policy queries to exclude deducted subscriptions:\n%s", sql)
			}
			for _, want := range []string{"user_id = 42", "subscribe_id = 10"} {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
		})
	}
}
