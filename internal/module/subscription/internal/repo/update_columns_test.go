package repo

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// TestUpdateSubscribeColumnsWritesOnlyNamedColumns pins the targeted update
// the quota task relies on: only the named columns (and updated_at) are set,
// on a locally managed row, and the row's cache keys are invalidated.
func TestUpdateSubscribeColumnsWritesOnlyNamedColumns(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		want      []string
		unwanted  []string
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			want:     []string{"UPDATE `user_subscribe` SET `expire_time`=", "`status`=5", "`finished_at`=NULL", "`updated_at`=", "WHERE id = 9 AND entitlement_source = ''"},
			unwanted: []string{"`upload`", "`download`", "`token`", "`uuid`", "`traffic`", "`subscribe_id`"},
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			want:     []string{`UPDATE "user_subscribe" SET "expire_time"=`, `"status"=5`, `"finished_at"=NULL`, `"updated_at"=`, "WHERE id = 9 AND entitlement_source = ''"},
			unwanted: []string{`"upload"`, `"download"`, `"token"`, `"uuid"`, `"traffic"`, `"subscribe_id"`},
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
			invalidations := cache.NewInvalidationQueue()
			subs := NewUserSubscriptionRepo(repository.ModuleConn{DB: db, Invalidations: invalidations}.Conn())
			sub := &usersub.Subscribe{
				Id: 9, UserId: 3, SubscribeId: 4, Status: usersub.SubscribeStatusStopped,
				ExpireTime: time.Now().Add(time.Hour), Traffic: 100, Upload: 7, Download: 8, Token: "tok", UUID: "uuid",
			}
			if err := subs.UpdateSubscribeColumns(context.Background(), sub, "expire_time", "status", "finished_at"); err != nil {
				t.Fatalf("UpdateSubscribeColumns: %v", err)
			}
			sql := logs.String()
			for _, want := range tt.want {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(sql, unwanted) {
					t.Fatalf("SQL writes unnamed column %s:\n%s", unwanted, sql)
				}
			}
			if keys := strings.Join(invalidations.Keys(), ","); !strings.Contains(keys, "cache:user:subscribe:id:9") || !strings.Contains(keys, "cache:user:subscribe:token:tok") {
				t.Fatalf("subscription cache keys not invalidated: %s", keys)
			}

			logs.Reset()
			if err := subs.UpdateSubscribeColumns(context.Background(), sub); err != nil || logs.Len() != 0 {
				t.Fatalf("no columns must be a no-op: err=%v sql=%s", err, logs.String())
			}
			// The provider owns a managed row's term; only local controls
			// (credentials, note, usage, hold) may be written.
			managed := *sub
			managed.EntitlementSource = "apple"
			if err := subs.UpdateSubscribeColumns(context.Background(), &managed, "status", "expire_time"); !errors.Is(err, usersub.ErrProviderManaged) || logs.Len() != 0 {
				t.Fatalf("provider-managed row: err=%v sql=%s", err, logs.String())
			}
		})
	}
}
