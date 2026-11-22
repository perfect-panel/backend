package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
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

func TestTrafficLogPaginationUsesStableNewestFirstOrder(t *testing.T) {
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
				t.Fatalf("open gorm db: %v", err)
			}

			var rows []*traffic.TrafficLog
			stmt := db.Model(&traffic.TrafficLog{}).
				Where("user_id = ? AND subscribe_id = ?", int64(7), int64(11)).
				Order(trafficLogNewestFirst).
				Limit(20).
				Find(&rows).Statement
			if sql := stmt.SQL.String(); !strings.Contains(sql, "ORDER BY timestamp DESC, id DESC") {
				t.Fatalf("traffic pagination is not deterministic:\n%s", sql)
			}
		})
	}
}

func TestQueryTrafficLogPageListCountsAndOrdersRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:traffic-pagination?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE traffic_log (
		id INTEGER PRIMARY KEY,
		server_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		subscribe_id INTEGER NOT NULL,
		download INTEGER NOT NULL DEFAULT 0,
		upload INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create traffic log table: %v", err)
	}

	older := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	rows := []*traffic.TrafficLog{
		{Id: 1, ServerId: 1, UserId: 7, SubscribeId: 11, Timestamp: older},
		{Id: 2, ServerId: 1, UserId: 7, SubscribeId: 11, Timestamp: newer},
		{Id: 3, ServerId: 1, UserId: 7, SubscribeId: 11, Timestamp: newer},
		{Id: 4, ServerId: 1, UserId: 8, SubscribeId: 11, Timestamp: newer},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("insert traffic logs: %v", err)
	}

	repo := &trafficRepo{Conn: db, table: "traffic"}
	got, total, err := repo.QueryTrafficLogPageList(context.Background(), 7, 11, 1, 2)
	if err != nil {
		t.Fatalf("query traffic logs: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	ids := make([]int64, 0, len(got))
	for _, row := range got {
		ids = append(ids, row.Id)
	}
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 2 {
		t.Fatalf("page ids = %v, want [3 2]", ids)
	}
}

func TestQueryTrafficLogDetailsOpenDateBounds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE traffic_log (id INTEGER PRIMARY KEY, server_id INTEGER, user_id INTEGER, subscribe_id INTEGER, download INTEGER, upload INTEGER, timestamp DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := db.Create(&traffic.TrafficLog{Id: int64(i + 1), Timestamp: day.AddDate(0, 0, i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name       string
		start, end time.Time
		want       int64
	}{
		{"both inclusive days", day, day.AddDate(0, 0, 2), 2},
		{"start only", day.AddDate(0, 0, 1), time.Time{}, 2},
		{"end only", time.Time{}, day.AddDate(0, 0, 1), 1},
		{"none", time.Time{}, time.Time{}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := (&trafficRepo{Conn: db}).QueryTrafficLogDetails(context.Background(), &traffic.TrafficLogDetailsFilter{Page: 1, Size: 10, Start: tc.start, End: tc.end})
			if err != nil || total != tc.want || len(rows) != int(tc.want) {
				t.Fatalf("total=%d rows=%d err=%v", total, len(rows), err)
			}
		})
	}
}
