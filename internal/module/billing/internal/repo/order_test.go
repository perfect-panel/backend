package repo

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestOrderRepoMarkOrderPaidUsesPendingCondition(t *testing.T) {
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

	updated, err := NewOrderRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()).MarkOrderPaid(context.Background(), "order-123", "trade-456")
	if err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}
	if updated {
		t.Fatal("dry-run update must not report an affected row")
	}
	sql := logs.String()
	// The transition binds the trade number: an order that claimed another
	// gateway payment is left alone.
	for _, want := range []string{"UPDATE `order`", "`status`=2", "`trade_no`='trade-456'",
		"WHERE order_no = 'order-123' AND status = 1 AND (trade_no IS NULL OR trade_no = '' OR trade_no = 'trade-456')"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestOrderRepoFindOneByOrderNoForUpdateUsesRowLock(t *testing.T) {
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

	if _, err := NewOrderRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()).FindOneByOrderNoForUpdate(context.Background(), "order-123"); err != nil {
		t.Fatalf("FindOneByOrderNoForUpdate: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"FROM `order`", "WHERE order_no = 'order-123'", "FOR UPDATE"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestOrderRepoUpdateOrderStatusFromUsesPendingCondition(t *testing.T) {
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

	updated, err := NewOrderRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()).UpdateOrderStatusFrom(context.Background(), "order-123", 1, 2)
	if err != nil {
		t.Fatalf("UpdateOrderStatusFrom: %v", err)
	}
	if updated {
		t.Fatal("dry-run update must not report an affected row")
	}
	sql := logs.String()
	for _, want := range []string{"UPDATE `order`", "`status`=2", "WHERE order_no = 'order-123' AND status = 1"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestOrderRepoUpdatePaymentExpectationOnlyInitializesSnapshot(t *testing.T) {
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

	if _, err := NewOrderRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()).UpdatePaymentExpectation(context.Background(), "order-123", 1000, "CNY"); err != nil {
		t.Fatalf("UpdatePaymentExpectation: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"UPDATE `order`", "`payment_amount`=1000", "`payment_currency`='CNY'", "WHERE order_no = 'order-123' AND status = 1 AND payment_currency = ''"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestApplyOrderListFiltersSearchSQL(t *testing.T) {
	tests := []struct {
		name       string
		dialector  gorm.Dialector
		wantSQL    []string
		wantNoSQL  []string
		wantOrder  string
		wantAuth   string
		wantSearch string
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			wantSQL: []string{
				"FROM `order`",
				"`order`.`status` = ?",
				"`order`.`user_id` = ?",
				"`order`.`subscribe_id` = ?",
				"`order`.`order_no` LIKE ? ESCAPE '='",
				"`order`.`trade_no` LIKE ? ESCAPE '='",
				"`order`.`coupon` LIKE ? ESCAPE '='",
				"EXISTS (SELECT 1 FROM `user_auth_methods`",
				"`user_auth_methods`.`user_id` = `order`.`user_id`",
				"`user_auth_methods`.`auth_type` = ?",
				"`user_auth_methods`.`auth_identifier` LIKE ? ESCAPE '='",
				"ORDER BY `order`.`id` desc",
			},
			wantNoSQL:  []string{"LEFT JOIN"},
			wantOrder:  "`order`",
			wantAuth:   "`user_auth_methods`",
			wantSearch: "alice=_100=%@example.com%",
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			wantSQL: []string{
				`FROM "order"`,
				`"order"."status" = $1`,
				`"order"."user_id" = $2`,
				`"order"."subscribe_id" = $3`,
				`"order"."order_no" LIKE $4 ESCAPE '='`,
				`"order"."trade_no" LIKE $5 ESCAPE '='`,
				`"order"."coupon" LIKE $6 ESCAPE '='`,
				`EXISTS (SELECT 1 FROM "user_auth_methods"`,
				`"user_auth_methods"."user_id" = "order"."user_id"`,
				`"user_auth_methods"."auth_type" = $7`,
				`"user_auth_methods"."auth_identifier" LIKE $8 ESCAPE '='`,
				`ORDER BY "order"."id" desc`,
			},
			wantNoSQL:  []string{"LEFT JOIN"},
			wantOrder:  `"order"`,
			wantAuth:   `"user_auth_methods"`,
			wantSearch: "alice=_100=%@example.com%",
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

			var result []order.Details
			query := applyOrderListFilters(db.Model(&order.Order{}), 2, 10, 20, "alice_100%@example.com")
			stmt := query.Order(orderColumn(query, "id") + " desc").Find(&result).Statement
			sql := stmt.SQL.String()

			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
			for _, unwanted := range tt.wantNoSQL {
				if strings.Contains(sql, unwanted) {
					t.Fatalf("SQL should not contain %q:\n%s", unwanted, sql)
				}
			}
			if got := orderTableName(db); got != tt.wantOrder {
				t.Fatalf("orderTableName() = %q, want %q", got, tt.wantOrder)
			}
			if got := orderQuoteTable(db, orderUserAuthMethodsTable); got != tt.wantAuth {
				t.Fatalf("orderQuoteTable(orderUserAuthMethodsTable) = %q, want %q", got, tt.wantAuth)
			}
			if len(stmt.Vars) != 8 {
				t.Fatalf("vars len = %d, want 8: %#v", len(stmt.Vars), stmt.Vars)
			}
			if got := stmt.Vars[3]; got != tt.wantSearch {
				t.Fatalf("order search pattern = %#v, want %#v", got, tt.wantSearch)
			}
			if got := stmt.Vars[6]; got != "email" {
				t.Fatalf("auth type = %#v, want email", got)
			}
			if got := stmt.Vars[7]; got != tt.wantSearch {
				t.Fatalf("email search pattern = %#v, want %#v", got, tt.wantSearch)
			}
		})
	}
}

func TestApplyOrderListFiltersSkipsBlankSearch(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	var result []order.Details
	stmt := applyOrderListFilters(db.Model(&order.Order{}), 0, 0, 0, "   ").Find(&result).Statement
	sql := stmt.SQL.String()
	if strings.Contains(sql, "LIKE") || strings.Contains(sql, "user_auth_methods") {
		t.Fatalf("blank search should not add search filters:\n%s", sql)
	}
	if len(stmt.Vars) != 0 {
		t.Fatalf("vars len = %d, want 0: %#v", len(stmt.Vars), stmt.Vars)
	}
}

// newDryRunOrderRepo returns an order repository whose SQL is rendered into
// the returned buffer instead of being executed. Times render in UTC.
func newDryRunOrderRepo(t *testing.T) (repository.OrderRepo, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=UTC",
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
	return NewOrderRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn()), &logs
}

// useAppLocation switches the application timezone for one test.
func useAppLocation(t *testing.T, name string) {
	t.Helper()
	previous := timeutil.LocationName()
	if err := timeutil.LoadLocation(name); err != nil {
		t.Skipf("timezone %s unavailable: %v", name, err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })
}

func TestOrderRepoCountPendingGuestOrdersFiltersByGuestIdentity(t *testing.T) {
	repo, logs := newDryRunOrderRepo(t)
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.CountPendingGuestOrders(context.Background(), "email", "guest@example.com", since); err != nil {
		t.Fatalf("CountPendingGuestOrders: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"SELECT count(*) FROM `order`", "WHERE user_id = 0 AND status = 1 AND guest_auth_type = 'email' AND guest_identifier = 'guest@example.com' AND created_at >= '2026-09-01 00:00:00'"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

// "Today" is the calendar day in the application timezone. 07:30 in Shanghai
// is still the previous day in UTC, which is the day Truncate(24h) picked.
func TestOrderRepoQueryDateOrdersUsesAppTimezoneDay(t *testing.T) {
	useAppLocation(t, "Asia/Shanghai")
	repo, logs := newDryRunOrderRepo(t)

	now := time.Date(2026, 9, 26, 23, 30, 0, 0, time.UTC) // 2026-09-27 07:30 in Shanghai
	// Scan cannot complete in dry-run mode; the rendered SQL is still logged.
	if _, err := repo.QueryDateOrders(context.Background(), now); err != nil && !errors.Is(err, gorm.ErrDryRunModeUnsupported) {
		t.Fatalf("QueryDateOrders: %v", err)
	}
	sql := logs.String()
	// Shanghai midnight of 2026-09-27 and 2026-09-28, rendered in UTC.
	for _, want := range []string{"created_at >= '2026-09-26 16:00:00'", "created_at < '2026-09-27 16:00:00'"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestOrderRepoQueryDailyOrdersListEndsAtAppTimezoneMidnight(t *testing.T) {
	useAppLocation(t, "Asia/Shanghai")
	repo, logs := newDryRunOrderRepo(t)

	now := time.Date(2026, 9, 26, 23, 30, 0, 0, time.UTC) // 2026-09-27 07:30 in Shanghai
	if _, err := repo.QueryDailyOrdersList(context.Background(), now); err != nil && !errors.Is(err, gorm.ErrDryRunModeUnsupported) {
		t.Fatalf("QueryDailyOrdersList: %v", err)
	}
	sql := logs.String()
	// Shanghai midnight of 2026-09-01 and 2026-09-28, rendered in UTC.
	for _, want := range []string{"created_at >= '2026-08-31 16:00:00'", "created_at < '2026-09-27 16:00:00'"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
}
