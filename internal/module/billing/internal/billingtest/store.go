// Package billingtest provides the billing module's behaviour-test harness:
// the real repository store over a private SQLite database and a
// miniredis instance, with seeding helpers for the rows order flows read.
// Only tests import it.
package billingtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/billing/internal/repo"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// Harness is a real repository store over a private in-memory database.
type Harness struct {
	t     testing.TB
	DB    *gorm.DB
	Redis *redis.Client
	Store *repository.GormStore
}

var databases atomic.Int64

// New opens a fresh database with the billing tables and the subscription
// (plans, user subscriptions, entitlement periods), identity and platform
// tables the order flows touch.
func New(t testing.TB) *Harness {
	t.Helper()
	// A file database in WAL mode lets a flow read outside its open
	// transaction, as it may against a production database: readers see the
	// last committed state and writers wait for each other.
	// The directory is not t.TempDir: test names may contain characters
	// such as '#' that end the path of a SQLite URI filename.
	dir, err := os.MkdirTemp("", "billingtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	name := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate",
		filepath.Join(dir, fmt.Sprintf("billing-%d.db", databases.Add(1))))
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{
		TranslateError:                   true,
		IgnoreRelationshipsWhenMigrating: true,
		Logger:                           gormlog.Default.LogMode(gormlog.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, model := range []any{
		&order.Order{}, &order.Event{}, &payment.Payment{}, &coupon.Coupon{},
		&wallet.Wallet{}, &wallet.Withdrawal{},
		&logEntity.SystemLog{}, &inbox.Record{}, &outbox.Event{},
		&subscribe.Subscribe{}, &user.User{}, &user.AuthMethods{}, &user.Device{},
		&entitlement.State{}, &entitlement.Period{}, &entitlement.Revision{},
	} {
		if err := db.AutoMigrate(model); err != nil {
			t.Fatalf("migrate %T: %v", model, err)
		}
		// SQLite index names are database-global, unlike the MySQL entity
		// tags several of these tables share.
		if db.Migrator().HasIndex(model, "idx_user_id") {
			if err := db.Migrator().RenameIndex(model, "idx_user_id", fmt.Sprintf("idx_user_id_%d", databases.Add(1))); err != nil {
				t.Fatalf("rename index of %T: %v", model, err)
			}
		}
	}
	// The user subscription model carries MySQL-only defaults; this is the
	// portable equivalent of its table.
	for _, ddl := range []string{
		`CREATE TABLE user_subscribe (
 id INTEGER PRIMARY KEY, user_id BIGINT, order_id BIGINT, subscribe_id BIGINT,
 start_time TIMESTAMP, expire_time TIMESTAMP, finished_at TIMESTAMP, traffic_reset_at TIMESTAMP,
 traffic BIGINT DEFAULT 0, download BIGINT DEFAULT 0, upload BIGINT DEFAULT 0,
 token VARCHAR(255) UNIQUE, uuid VARCHAR(255) UNIQUE, status INTEGER DEFAULT 0,
 note TEXT DEFAULT '', entitlement_source VARCHAR(32) NOT NULL DEFAULT '', created_at TIMESTAMP, updated_at TIMESTAMP)`,
		`CREATE TABLE subscription_user_serial (user_id INTEGER PRIMARY KEY)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return &Harness{
		t:     t,
		DB:    db,
		Redis: client,
		Store: repository.NewGormStoreWithBuilders(db, client, repository.Builders{
			Platform:     platform.NewRepoBuilder(),
			Billing:      repo.NewBuilder(),
			Subscription: subscription.NewRepoBuilder(),
			Identity:     identity.NewRepoBuilder(),
			Network:      network.NewRepoBuilder(client),
			Support:      support.NewRepoBuilder(),
			Notification: notification.NewRepoBuilder(),
		}),
	}
}

func (h *Harness) create(value any) {
	h.t.Helper()
	if err := h.DB.Create(value).Error; err != nil {
		h.t.Fatalf("seed %T: %v", value, err)
	}
}

// Payment seeds an enabled payment method of platform with a configuration.
func (h *Harness) Payment(platform, config string, adjust ...func(*payment.Payment)) *payment.Payment {
	h.t.Helper()
	enabled := true
	method := &payment.Payment{
		Name: platform, Platform: platform, Config: config, Enable: &enabled,
		Token: fmt.Sprintf("%s-token-%d", strings.ToLower(platform), databases.Add(1)),
	}
	for _, fn := range adjust {
		fn(method)
	}
	h.create(method)
	return method
}

// Plan seeds a plan on sale with unlimited stock.
func (h *Harness) Plan(unitPrice int64, adjust ...func(*subscribe.Subscribe)) *subscribe.Subscribe {
	h.t.Helper()
	sell, show := true, true
	plan := &subscribe.Subscribe{
		Name: fmt.Sprintf("plan-%d", databases.Add(1)), UnitPrice: unitPrice, UnitTime: "Month",
		Inventory: -1, Sell: &sell, Show: &show,
	}
	for _, fn := range adjust {
		fn(plan)
	}
	h.create(plan)
	return plan
}

// User seeds an account.
func (h *Harness) User(adjust ...func(*user.User)) *user.User {
	h.t.Helper()
	enabled := true
	account := &user.User{Enable: &enabled}
	for _, fn := range adjust {
		fn(account)
	}
	h.create(account)
	return account
}

// Wallet seeds the wallet of userID.
func (h *Harness) Wallet(userID, balance, gift int64) {
	h.t.Helper()
	h.create(&wallet.Wallet{UserId: userID, Balance: balance, GiftAmount: gift})
}

// Coupon seeds an enabled coupon valid for the next hour.
func (h *Harness) Coupon(code string, adjust ...func(*coupon.Coupon)) *coupon.Coupon {
	h.t.Helper()
	enabled := true
	now := timeutil.Now()
	c := &coupon.Coupon{
		Name: code, Code: code, Type: coupon.TypeFixed, Discount: 100, Enable: &enabled,
		StartTime: now.Add(-time.Hour).UnixMilli(), ExpireTime: now.Add(time.Hour).UnixMilli(),
	}
	for _, fn := range adjust {
		fn(c)
	}
	h.create(c)
	return c
}

// UserSubscription seeds a subscription of userID to plan.
func (h *Harness) UserSubscription(userID int64, plan *subscribe.Subscribe, adjust ...func(*usersub.Subscribe)) *usersub.Subscribe {
	h.t.Helper()
	sub := &usersub.Subscribe{
		UserId: userID, SubscribeId: plan.Id, Status: usersub.SubscribeStatusActive,
		Token:      fmt.Sprintf("token-%d", databases.Add(1)),
		UUID:       fmt.Sprintf("uuid-%d", databases.Add(1)),
		StartTime:  timeutil.Now().Add(-time.Hour),
		ExpireTime: timeutil.Now().Add(30 * 24 * time.Hour),
	}
	for _, fn := range adjust {
		fn(sub)
	}
	h.create(sub)
	return sub
}

// Order seeds an order row directly, without its creation event.
func (h *Harness) Order(o *order.Order) *order.Order {
	h.t.Helper()
	if o.StateVersion == 0 {
		o.StateVersion = 1
	}
	h.create(o)
	return o
}

// ReloadOrder reads the order back from the database.
func (h *Harness) ReloadOrder(orderNo string) *order.Order {
	h.t.Helper()
	var o order.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		h.t.Fatalf("reload order %s: %v", orderNo, err)
	}
	return &o
}

// Orders counts the orders of userID.
func (h *Harness) Orders(userID int64) []*order.Order {
	h.t.Helper()
	var orders []*order.Order
	if err := h.DB.Where("user_id = ?", userID).Order("id ASC").Find(&orders).Error; err != nil {
		h.t.Fatal(err)
	}
	return orders
}

// ReloadWallet reads the wallet of userID; a missing row reads as zero.
func (h *Harness) ReloadWallet(userID int64) wallet.Wallet {
	h.t.Helper()
	var w wallet.Wallet
	err := h.DB.Where("user_id = ?", userID).Take(&w).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.t.Fatal(err)
	}
	return w
}

// ReloadCoupon reads the coupon back.
func (h *Harness) ReloadCoupon(code string) *coupon.Coupon {
	h.t.Helper()
	var c coupon.Coupon
	if err := h.DB.Where("code = ?", code).First(&c).Error; err != nil {
		h.t.Fatal(err)
	}
	return &c
}

// ReloadPlan reads the plan back.
func (h *Harness) ReloadPlan(id int64) *subscribe.Subscribe {
	h.t.Helper()
	var plan subscribe.Subscribe
	if err := h.DB.First(&plan, id).Error; err != nil {
		h.t.Fatal(err)
	}
	return &plan
}

// Logs returns the audit records of kind for objectID in insertion order.
func (h *Harness) Logs(kind logEntity.Type, objectID int64) []*logEntity.SystemLog {
	h.t.Helper()
	var logs []*logEntity.SystemLog
	if err := h.DB.Where("type = ? AND object_id = ?", kind.Uint8(), objectID).Order("id ASC").Find(&logs).Error; err != nil {
		h.t.Fatal(err)
	}
	return logs
}

// GiftLogs decodes the gift ledger of userID.
func (h *Harness) GiftLogs(userID int64) []logEntity.Gift {
	h.t.Helper()
	var gifts []logEntity.Gift
	for _, entry := range h.Logs(logEntity.TypeGift, userID) {
		var gift logEntity.Gift
		if err := gift.Unmarshal([]byte(entry.Content)); err != nil {
			h.t.Fatal(err)
		}
		gifts = append(gifts, gift)
	}
	return gifts
}

// BalanceLogs decodes the balance ledger of userID.
func (h *Harness) BalanceLogs(userID int64) []logEntity.Balance {
	h.t.Helper()
	var entries []logEntity.Balance
	for _, entry := range h.Logs(logEntity.TypeBalance, userID) {
		var balance logEntity.Balance
		if err := balance.Unmarshal([]byte(entry.Content)); err != nil {
			h.t.Fatal(err)
		}
		entries = append(entries, balance)
	}
	return entries
}

// Events lists the order events of orderNo.
func (h *Harness) Events(orderNo string) []order.Event {
	h.t.Helper()
	var events []order.Event
	if err := h.DB.Where("order_no = ?", orderNo).Order("id ASC").Find(&events).Error; err != nil {
		h.t.Fatal(err)
	}
	return events
}

// UserContext returns a request context authenticated as u.
func UserContext(u *user.User) context.Context {
	return user.NewContext(context.Background(), u)
}

// Queue records the order lifecycle tasks the flows schedule.
type Queue struct {
	Activations    []string
	DeferredCloses []string
	ActivationErr  error
}

func (q *Queue) EnqueueActivation(_ context.Context, orderNo string) error {
	if q.ActivationErr != nil {
		return q.ActivationErr
	}
	q.Activations = append(q.Activations, orderNo)
	return nil
}

func (q *Queue) EnqueueDeferredClose(_ context.Context, orderNo string) error {
	q.DeferredCloses = append(q.DeferredCloses, orderNo)
	return nil
}

// UserCache stands in for the identity module's user cache: it records the
// users whose cached projection a flow dropped.
type UserCache struct {
	Cleared []int64
}

func (c *UserCache) ClearUserCache(_ context.Context, userIDs ...int64) error {
	c.Cleared = append(c.Cleared, userIDs...)
	return nil
}
