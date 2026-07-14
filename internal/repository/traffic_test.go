package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/traffic"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTrafficAggregateSQL(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		want      []string
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			want: []string{
				"COALESCE(SUM(`traffic_log`.`download`), 0) AS download",
				"COALESCE(SUM(`traffic_log`.`upload`), 0) AS upload",
				"`traffic_log`.`timestamp` >= ? AND `traffic_log`.`timestamp` < ?",
			},
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			want: []string{
				`COALESCE(SUM("traffic_log"."download"), 0)::bigint AS download`,
				`COALESCE(SUM("traffic_log"."upload"), 0)::bigint AS upload`,
				`"traffic_log"."timestamp" >= $1 AND "traffic_log"."timestamp" < $2`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
			})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var result traffic.TotalTraffic
			start := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			stmt := db.Model(&traffic.TrafficLog{}).
				Select(totalTrafficSelect(db)).
				Where(trafficTimeRangeCondition(db), start, end).
				Scan(&result).Statement
			sql := stmt.SQL.String()

			for _, want := range tt.want {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
		})
	}
}

func TestTrafficRankingSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	var result []traffic.ServerTrafficRanking
	start := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	stmt := db.Model(&traffic.TrafficLog{}).
		Select(serverTrafficRankingSelect(db)).
		Where(trafficTimeRangeCondition(db), start, end).
		Group(trafficColumn(db, "server_id")).
		Order("total DESC").
		Scan(&result).Statement
	sql := stmt.SQL.String()

	want := []string{
		`"traffic_log"."server_id" AS server_id`,
		`COALESCE(SUM("traffic_log"."download" + "traffic_log"."upload"), 0)::bigint AS total`,
		`GROUP BY "traffic_log"."server_id"`,
		`ORDER BY total DESC`,
	}
	for _, item := range want {
		if !strings.Contains(sql, item) {
			t.Fatalf("SQL missing %q:\n%s", item, sql)
		}
	}
}
