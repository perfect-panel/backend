// Package subtest is the subscription module's behaviour-test fixture: a
// SQLite database with the module's tables and a miniredis cache behind the
// module's real repositories. InSubscriptionTx runs in a real transaction and
// invalidates the queued cache keys after the commit, like the production
// store. Only tests import this package.
package subtest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// userSubscribeTable is the portable equivalent of the production table; the
// model carries MySQL-specific type and default tags.
const userSubscribeTable = `CREATE TABLE user_subscribe (
 id INTEGER PRIMARY KEY, user_id BIGINT, order_id BIGINT, subscribe_id BIGINT,
 start_time TIMESTAMP, expire_time TIMESTAMP, finished_at TIMESTAMP, traffic_reset_at TIMESTAMP,
 traffic BIGINT DEFAULT 0, download BIGINT DEFAULT 0, upload BIGINT DEFAULT 0,
 token VARCHAR(255) UNIQUE, uuid VARCHAR(255) UNIQUE, status INTEGER DEFAULT 0,
 note TEXT DEFAULT '', entitlement_source VARCHAR(32) NOT NULL DEFAULT '', created_at TIMESTAMP, updated_at TIMESTAMP)`

// Fixture is one test's database, cache and store.
type Fixture struct {
	DB    *gorm.DB
	Redis *redis.Client
	Mini  *miniredis.Miniredis
	Store *Store
}

// New opens a fresh fixture that the test cleans up.
func New(t testing.TB) *Fixture {
	t.Helper()
	// A writer waits for another connection's transaction instead of
	// failing at once, as it would on a server database.
	dsn := filepath.Join(t.TempDir(), "subscription.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(userSubscribeTable).Error; err != nil {
		t.Fatalf("create user_subscribe: %v", err)
	}
	if err := db.Exec("CREATE TABLE subscription_user_serial (user_id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&subscribe.Subscribe{}, &subscribe.Group{}, &log.SystemLog{}, &inbox.Record{},
		&entitlement.State{}, &entitlement.Period{}, &entitlement.Revision{}} {
		if err := db.AutoMigrate(model); err != nil {
			t.Fatalf("migrate %T: %v", model, err)
		}
	}
	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	return &Fixture{DB: db, Redis: rds, Mini: mini, Store: &Store{db: db, rds: rds, failCommits: new(atomic.Int32), commitsBeforeFailure: new(atomic.Int32)}}
}

// Plan inserts a plan with the given reset cycle and traffic quota.
func (f *Fixture) Plan(t testing.TB, plan subscribe.Subscribe) *subscribe.Subscribe {
	t.Helper()
	if plan.UnitTime == "" {
		plan.UnitTime = "Month"
	}
	if plan.Name == "" {
		plan.Name = fmt.Sprintf("plan-%d", plan.Id)
	}
	if err := f.DB.Create(&plan).Error; err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return &plan
}

var tokens atomic.Int64

// Subscription inserts a subscription row; unset credentials get unique ones.
func (f *Fixture) Subscription(t testing.TB, sub usersub.Subscribe) *usersub.Subscribe {
	t.Helper()
	n := tokens.Add(1)
	if sub.Token == "" {
		sub.Token = fmt.Sprintf("token-%d", n)
	}
	if sub.UUID == "" {
		sub.UUID = fmt.Sprintf("uuid-%d", n)
	}
	if sub.StartTime.IsZero() {
		sub.StartTime = time.Now().Add(-24 * time.Hour)
	}
	if err := f.DB.Create(&sub).Error; err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	return &sub
}

// Load reads a subscription row as stored.
func (f *Fixture) Load(t testing.TB, id int64) *usersub.Subscribe {
	t.Helper()
	var sub usersub.Subscribe
	if err := f.DB.First(&sub, id).Error; err != nil {
		t.Fatalf("load subscription %d: %v", id, err)
	}
	return &sub
}

// Logs returns the system log rows of a type.
func (f *Fixture) Logs(t testing.TB, typ log.Type) []log.SystemLog {
	t.Helper()
	var rows []log.SystemLog
	if err := f.DB.Where("type = ?", typ.Uint8()).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// Cached reports whether the cache holds key.
func (f *Fixture) Cached(key string) bool {
	return f.Mini.Exists(key)
}

// Store is the fixture's persistence: the subscription domain's repositories
// and scoped transaction, the inbox and the audit log.
type Store struct {
	db            *gorm.DB
	rds           *redis.Client
	invalidations *cache.InvalidationQueue
	// failCommits makes the next that many subscription transactions roll
	// back after their closure succeeded; commitsBeforeFailure lets that many
	// commit first.
	failCommits          *atomic.Int32
	commitsBeforeFailure *atomic.Int32
}

var (
	_ repository.SubscriptionStore      = (*Store)(nil)
	_ repository.SubscriptionTransactor = (*Store)(nil)
)

// ErrInjectedRollback is the error of a transaction FailNextCommits rolled
// back.
var ErrInjectedRollback = errors.New("injected transaction rollback")

// FailNextCommits rolls back the next n subscription transactions after
// their work succeeded, as a lost connection at commit would.
func (s *Store) FailNextCommits(n int) {
	s.FailCommitsAfter(0, n)
}

// FailCommitsAfter lets the next committed transactions commit, then rolls
// back the n after them.
func (s *Store) FailCommitsAfter(committed, n int) {
	s.commitsBeforeFailure.Store(int32(committed))
	s.failCommits.Store(int32(n))
}

// InSubscriptionTx runs fn in a transaction and invalidates the cache keys
// its writes queued once it commits.
func (s *Store) InSubscriptionTx(ctx context.Context, fn func(repository.SubscriptionStore) error) error {
	queue := cache.NewInvalidationQueue()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := &Store{db: tx, rds: s.rds, invalidations: queue, failCommits: s.failCommits, commitsBeforeFailure: s.commitsBeforeFailure}
		if err := fn(scoped); err != nil {
			return err
		}
		if s.failCommits.Load() > 0 {
			if s.commitsBeforeFailure.Load() > 0 {
				s.commitsBeforeFailure.Add(-1)
				return nil
			}
			s.failCommits.Add(-1)
			return ErrInjectedRollback
		}
		return nil
	})
	if err != nil {
		return err
	}
	return queue.Flush(ctx, s.rds)
}

func (s *Store) conn() cache.CachedConn {
	return repository.ModuleConn{DB: s.db, Redis: s.rds, Invalidations: s.invalidations}.Conn()
}

func (s *Store) UserSubscription() repository.UserSubscriptionRepo {
	return repo.NewUserSubscriptionRepo(s.conn())
}

func (s *Store) SubscriptionTraffic() repository.SubscriptionTrafficRepo {
	return repo.NewUserSubscriptionRepo(s.conn())
}

func (s *Store) Subscribe() repository.SubscribeRepo {
	return repo.NewSubscribeRepo(s.conn(), nil)
}

func (s *Store) Entitlement() repository.EntitlementRepo {
	return repo.NewEntitlementRepo(s.db)
}

func (s *Store) Inbox() repository.InboxRepo { return &Inbox{db: s.db} }

func (s *Store) Log() repository.LogRepo { return &Logs{db: s.db} }

// Outbox is not part of the subscription flows under test.
func (s *Store) Outbox() repository.OutboxRepo { return nil }

// NewInbox returns the inbox over db (a fixture database or a transaction).
func NewInbox(db *gorm.DB) *Inbox { return &Inbox{db: db} }

// NewLogs returns the audit log over db (a fixture database or a
// transaction).
func NewLogs(db *gorm.DB) *Logs { return &Logs{db: db} }

// Inbox is a SQLite-backed repository.InboxRepo.
type Inbox struct{ db *gorm.DB }

var _ repository.InboxRepo = (*Inbox)(nil)

func (r *Inbox) Find(ctx context.Context, consumer, eventKey string) (*inbox.Record, error) {
	var row inbox.Record
	err := r.db.WithContext(ctx).First(&row, "consumer = ? AND event_key = ?", consumer, eventKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *Inbox) Insert(ctx context.Context, consumer, eventKey, result string) error {
	return r.db.WithContext(ctx).Create(&inbox.Record{Consumer: consumer, EventKey: eventKey, Result: result}).Error
}

func (r *Inbox) DeleteProcessedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("processed_at < ?", cutoff).Delete(&inbox.Record{})
	return result.RowsAffected, result.Error
}

// Logs is a SQLite-backed repository.LogRepo for the writes the subscription
// flows make; the platform module's reporting queries are not supported.
type Logs struct{ db *gorm.DB }

var _ repository.LogRepo = (*Logs)(nil)

var errUnsupported = errors.New("not supported by the subscription test fixture")

func (r *Logs) Insert(ctx context.Context, data *log.SystemLog) error {
	return r.db.WithContext(ctx).Create(data).Error
}

func (r *Logs) InsertBatch(ctx context.Context, data []*log.SystemLog, batchSize int) error {
	if len(data) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(data, max(batchSize, 1)).Error
}

func (r *Logs) FindOne(ctx context.Context, id int64) (*log.SystemLog, error) {
	var row log.SystemLog
	return &row, r.db.WithContext(ctx).First(&row, id).Error
}

func (r *Logs) FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error) {
	query := r.db.WithContext(ctx).Model(&log.SystemLog{}).Where("type = ?", filter.Type)
	if filter.ObjectID != 0 {
		query = query.Where("object_id = ?", filter.ObjectID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*log.SystemLog
	size := max(filter.Size, 1)
	err := query.Order("id DESC").Limit(size).Offset(max(filter.Page-1, 0) * size).Find(&rows).Error
	return rows, total, err
}

func (r *Logs) Update(context.Context, *log.SystemLog) error { return errUnsupported }
func (r *Logs) Delete(context.Context, int64) error          { return errUnsupported }
func (r *Logs) FindFirstByDateType(context.Context, string, uint8) (*log.SystemLog, error) {
	return nil, errUnsupported
}
func (r *Logs) FindByDatesType(context.Context, []string, uint8) ([]*log.SystemLog, error) {
	return nil, errUnsupported
}
func (r *Logs) DeleteBefore(context.Context, time.Time) error { return errUnsupported }
func (r *Logs) DeleteBeforeBatch(context.Context, time.Time, int) (int64, error) {
	return 0, errUnsupported
}
func (r *Logs) SumAmountByTypeAndObjectID(context.Context, uint8, int64) (int64, error) {
	return 0, errUnsupported
}
