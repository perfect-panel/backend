package repository

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestOrderRepoUpdateOrderStatusAndTradeNoUpdatesBothFields(t *testing.T) {
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger: gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{
			LogLevel: gormlogger.Info,
		}),
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	repo := newOrderRepo(db, redisClient)

	err = repo.UpdateOrderStatusAndTradeNo(context.Background(), "order-123", 2, "trade-456")
	if err != nil {
		t.Fatalf("update order status and trade number: %v", err)
	}

	sql := logs.String()
	for _, want := range []string{"UPDATE `order`", "`status`=2", "`trade_no`='trade-456'", "WHERE order_no = 'order-123'"} {
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
