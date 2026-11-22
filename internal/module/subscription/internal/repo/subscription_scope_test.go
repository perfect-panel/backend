package repo

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func scopeTestRepo(t *testing.T) (*UserSubscriptionRepo, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
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
	return NewUserSubscriptionRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()), &logs
}

// TestSubscriptionUserIDsTokenMatchesTokenOrUUID pins the semantics the
// identity admin filter used to encode in its EXISTS subquery: a token
// matches the subscription token or UUID and adds no status constraint.
func TestSubscriptionUserIDsTokenMatchesTokenOrUUID(t *testing.T) {
	repo, logs := scopeTestRepo(t)
	_, err := repo.SubscriptionUserIDs(context.Background(), repository.SubscriptionUserFilter{Token: "sub-token"})
	if err != nil {
		t.Fatalf("SubscriptionUserIDs: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"DISTINCT `user_id`", "(token = 'sub-token' OR uuid = 'sub-token')"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "status") {
		t.Fatalf("token/uuid lookup must not filter by status:\n%s", sql)
	}
}

// TestSubscriptionUserIDsAppliesIdAndStatusFilters pins the tokenless shape:
// row id, plan id and the caller's status set all constrain the query.
func TestSubscriptionUserIDsAppliesIdAndStatusFilters(t *testing.T) {
	repo, logs := scopeTestRepo(t)
	rowID, planID := int64(10), int64(20)
	_, err := repo.SubscriptionUserIDs(context.Background(), repository.SubscriptionUserFilter{
		UserSubscribeID: &rowID,
		SubscribeID:     &planID,
		Statuses:        []int64{0, 1},
	})
	if err != nil {
		t.Fatalf("SubscriptionUserIDs: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"DISTINCT `user_id`", "id = 10", "subscribe_id = 20", "status IN (0,1)"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}
