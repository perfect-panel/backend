package repo

import (
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestActiveLifecycleQueryMatchesPostgresPartialIndexPredicate(t *testing.T) {
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
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
			})
			if err != nil {
				t.Fatalf("open dry-run database: %v", err)
			}

			var rows []*usersub.Subscribe
			expired, args := usersub.ExpiredCondition(time.Now())
			stmt := activeLifecycleSubscribes(db).
				Where(expired, args...).
				Find(&rows).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "status IN (0, 1) AND finished_at IS NULL") {
				t.Fatalf("lifecycle SQL does not imply the partial-index predicate:\n%s", sql)
			}
			if len(stmt.Vars) != 2 {
				t.Fatalf("lifecycle SQL has %d parameters, want only the two expiry values: %s", len(stmt.Vars), sql)
			}
		})
	}
}
