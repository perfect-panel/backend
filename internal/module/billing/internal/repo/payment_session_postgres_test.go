package repo

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The DSN must name a disposable test database. A private schema scopes every
// table and is removed after the test; existing application tables are untouched.
func TestPaymentSessionsPostgres(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_EPAY_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_EPAY_POSTGRES_DSN for PostgreSQL payment session concurrency")
	}
	ctx := context.Background()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	rootConn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootConn.Close() })
	schema := fmt.Sprintf("epay_session_%d", time.Now().UnixNano())
	if err := db.WithContext(ctx).Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.WithContext(ctx).Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	params := u.Query()
	params.Set("search_path", schema)
	u.RawQuery = params.Encode()
	isolated, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := isolated.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := isolated.WithContext(ctx).Exec(`CREATE TABLE "order" (
id bigserial PRIMARY KEY, user_id bigint NOT NULL DEFAULT 0,
order_no text UNIQUE NOT NULL, status smallint NOT NULL DEFAULT 1,
payment_amount bigint NOT NULL DEFAULT 1000, payment_currency text NOT NULL DEFAULT 'CNY',
trade_no text UNIQUE, payment_session text, updated_at timestamptz
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := isolated.WithContext(ctx).Exec(`INSERT INTO "order" (order_no) VALUES ('first'), ('second')`).Error; err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	repo := NewOrderRepo(repository.ModuleConn{DB: isolated, Redis: redisClient}.Conn())
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			ok, err := repo.BeginPaymentSession(ctx, "first")
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("concurrent create attempts=%d, want one", winners.Load())
	}
	if ok, err := repo.CompletePaymentSession(ctx, "first", "trade-1", `{"state":"ready"}`); err != nil || !ok {
		t.Fatalf("complete: %t %v", ok, err)
	}
	if ok, err := repo.BeginPaymentSession(ctx, "second"); err != nil || !ok {
		t.Fatalf("second begin: %t %v", ok, err)
	}
	if ok, err := repo.CompletePaymentSession(ctx, "second", "trade-1", `{"state":"ready"}`); err == nil || ok {
		t.Fatal("duplicate provider trade was accepted")
	}
	second, err := repo.FindOneByOrderNo(ctx, "second")
	if err != nil || second.PaymentSession != order.PaymentSessionStarting || second.TradeNo != "" {
		t.Fatalf("failed completion partially committed: %v", err)
	}
}
