package repo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// writeFixture is a SQLite user_subscribe/subscribe database with a
// miniredis cache behind the real repositories.
type writeFixture struct {
	db   *gorm.DB
	mini *miniredis.Miniredis
	rds  *redis.Client
}

func newWriteFixture(t *testing.T) *writeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "writes.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE user_subscribe (
 id INTEGER PRIMARY KEY, user_id BIGINT, order_id BIGINT, subscribe_id BIGINT,
 start_time TIMESTAMP, expire_time TIMESTAMP, finished_at TIMESTAMP, traffic_reset_at TIMESTAMP,
 traffic BIGINT DEFAULT 0, download BIGINT DEFAULT 0, upload BIGINT DEFAULT 0,
 token VARCHAR(255) UNIQUE, uuid VARCHAR(255) UNIQUE, status INTEGER DEFAULT 0,
 note TEXT DEFAULT '', entitlement_source VARCHAR(32) NOT NULL DEFAULT '', created_at TIMESTAMP, updated_at TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&subscribe.Subscribe{}); err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	return &writeFixture{db: db, mini: mini, rds: rds}
}

func (f *writeFixture) subs() *UserSubscriptionRepo {
	return NewUserSubscriptionRepo(repository.ModuleConn{DB: f.db, Redis: f.rds}.Conn())
}

func (f *writeFixture) insert(t *testing.T, sub usersub.Subscribe) *usersub.Subscribe {
	t.Helper()
	if sub.Token == "" {
		sub.Token = fmt.Sprintf("token-%d", sub.Id)
	}
	if sub.UUID == "" {
		sub.UUID = fmt.Sprintf("uuid-%d", sub.Id)
	}
	if err := f.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	return &sub
}

func (f *writeFixture) load(t *testing.T, id int64) *usersub.Subscribe {
	t.Helper()
	var sub usersub.Subscribe
	if err := f.db.First(&sub, id).Error; err != nil {
		t.Fatal(err)
	}
	return &sub
}

// Provider-managed rows accept only the local controls, and are reactivated
// only inside their provider period.
func TestUpdateSubscribeColumnsGuardsProviderManagedRows(t *testing.T) {
	f := newWriteFixture(t)
	ctx := context.Background()
	subs := f.subs()
	now := time.Now()
	current := f.insert(t, usersub.Subscribe{Id: 1, UserId: 7, EntitlementSource: "apple", StartTime: now.Add(-time.Hour), ExpireTime: now.Add(time.Hour), Status: usersub.SubscribeStatusStopped})
	lapsed := f.insert(t, usersub.Subscribe{Id: 2, UserId: 7, EntitlementSource: "apple", StartTime: now.Add(-2 * time.Hour), ExpireTime: now.Add(-time.Hour), Status: usersub.SubscribeStatusStopped})

	current.ExpireTime = now.Add(24 * time.Hour)
	if err := subs.UpdateSubscribeColumns(ctx, current, "expire_time"); !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("provider term write = %v", err)
	}
	current.Note, current.Status = "note", usersub.SubscribeStatusActive
	if err := subs.UpdateSubscribeColumns(ctx, current, "note", "status"); err != nil {
		t.Fatalf("local controls on a current provider row: %v", err)
	}
	if got := f.load(t, 1); got.Note != "note" || got.Status != usersub.SubscribeStatusActive || !got.ExpireTime.Equal(now.Add(time.Hour)) {
		t.Fatalf("provider row after local controls: %+v", got)
	}
	lapsed.Status = usersub.SubscribeStatusActive
	if err := subs.UpdateSubscribeColumns(ctx, lapsed, "status"); !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("reactivating a lapsed provider period = %v", err)
	}
	// A stale copy that still shows the period current is stopped by the
	// statement's own guard.
	stale := *lapsed
	stale.ExpireTime = now.Add(time.Hour)
	if err := subs.UpdateSubscribeColumns(ctx, &stale, "status"); err != nil {
		t.Fatal(err)
	}
	if got := f.load(t, 2); got.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("stale copy reactivated a lapsed provider row: %+v", got)
	}
	if err := subs.UpdateSubscribeColumns(ctx, &usersub.Subscribe{}, "note"); !errors.Is(err, errSubscriptionID) {
		t.Fatalf("zero id column write = %v", err)
	}
}

func TestRotateSubscribeCredentialsBatchesAndInvalidatesBothTokens(t *testing.T) {
	f := newWriteFixture(t)
	ctx := context.Background()
	subs := f.subs()
	var rotations []repository.SubscriptionCredentialRotation
	for id := int64(1); id <= batchUpdateSize+3; id++ {
		sub := f.insert(t, usersub.Subscribe{Id: id, UserId: 7, Status: usersub.SubscribeStatusActive, Upload: id, Note: "kept"})
		rotations = append(rotations, repository.SubscriptionCredentialRotation{Previous: sub, Token: fmt.Sprintf("new-token-%d", id), UUID: fmt.Sprintf("new-uuid-%d", id)})
	}
	if _, err := subs.FindOneSubscribeByToken(ctx, "token-2"); err != nil {
		t.Fatal(err)
	}
	if err := subs.RotateSubscribeCredentials(ctx, rotations); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, batchUpdateSize + 3} {
		got := f.load(t, id)
		if got.Token != fmt.Sprintf("new-token-%d", id) || got.UUID != fmt.Sprintf("new-uuid-%d", id) || got.Upload != id || got.Note != "kept" {
			t.Fatalf("row %d after rotation: %+v", id, got)
		}
	}
	if f.mini.Exists("cache:user:subscribe:token:token-2") {
		t.Fatal("the previous token's cache entry survived the rotation")
	}
}

func TestFindSubscribeDetailsByIdsLoadsPlans(t *testing.T) {
	f := newWriteFixture(t)
	if err := f.db.Create(&subscribe.Subscribe{Id: 3, Name: "gold"}).Error; err != nil {
		t.Fatal(err)
	}
	f.insert(t, usersub.Subscribe{Id: 1, UserId: 7, SubscribeId: 3})
	f.insert(t, usersub.Subscribe{Id: 2, UserId: 8, SubscribeId: 3})
	got, err := f.subs().FindSubscribeDetailsByIds(context.Background(), []int64{1, 2, 99})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Id < got[j].Id })
	if len(got) != 2 || got[0].Subscribe == nil || got[0].Subscribe.Name != "gold" || got[1].UserId != 8 {
		t.Fatalf("details = %+v", got)
	}
	if empty, err := f.subs().FindSubscribeDetailsByIds(context.Background(), nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids = %v, %v", empty, err)
	}
}

// The calendar reset clears each due subscription once per day: the marker
// it writes keeps a repeated run away, and it reactivates exhausted rows.
func TestTrafficResetMarksEachSubscriptionOncePerDay(t *testing.T) {
	f := newWriteFixture(t)
	ctx := context.Background()
	subs := f.subs()
	now := time.Now()
	day := now.Truncate(24 * time.Hour)
	yesterday := day.Add(-24 * time.Hour)
	future, past := now.Add(24*time.Hour), now.Add(-time.Hour)
	started := now.Add(-48 * time.Hour)
	f.insert(t, usersub.Subscribe{Id: 1, SubscribeId: 1, StartTime: started, ExpireTime: future, Status: usersub.SubscribeStatusActive, Upload: 5})
	f.insert(t, usersub.Subscribe{Id: 2, SubscribeId: 1, StartTime: started, ExpireTime: future, Status: usersub.SubscribeStatusFinished, Traffic: 10, Upload: 10, FinishedAt: &past})
	f.insert(t, usersub.Subscribe{Id: 3, SubscribeId: 1, StartTime: started, ExpireTime: usersub.NoLimitExpiry(), Status: usersub.SubscribeStatusActive, Upload: 5, TrafficResetAt: &yesterday})
	f.insert(t, usersub.Subscribe{Id: 4, SubscribeId: 1, StartTime: started, ExpireTime: past, Status: usersub.SubscribeStatusActive, Upload: 5})
	f.insert(t, usersub.Subscribe{Id: 5, SubscribeId: 1, StartTime: started, ExpireTime: future, Status: usersub.SubscribeStatusStopped, Upload: 5})
	f.insert(t, usersub.Subscribe{Id: 6, SubscribeId: 1, StartTime: future, ExpireTime: future.Add(time.Hour), Status: usersub.SubscribeStatusActive, Upload: 5})
	f.insert(t, usersub.Subscribe{Id: 7, SubscribeId: 2, StartTime: started, ExpireTime: future, Status: usersub.SubscribeStatusActive, Upload: 5})
	f.insert(t, usersub.Subscribe{Id: 8, SubscribeId: 1, StartTime: started, ExpireTime: future, Status: usersub.SubscribeStatusActive, Upload: 5, TrafficResetAt: &day})

	candidates, err := subs.FindTrafficResetCandidates(ctx, []int64{1}, now, day)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.Id)
		if c.StartTime.IsZero() || c.SubscribeId != 1 {
			t.Fatalf("candidate lacks what the cycle needs: %+v", c)
		}
		// The catch-up rule compares the last reset day with the marker.
		if c.Id == 3 && (c.TrafficResetAt == nil || !c.TrafficResetAt.Equal(yesterday)) {
			t.Fatalf("candidate 3 lacks its reset marker: %+v", c)
		}
	}
	if !reflect.DeepEqual(ids, []int64{1, 2, 3}) {
		t.Fatalf("candidates = %v, want [1 2 3]", ids)
	}

	reset, err := subs.ResetSubscribeTrafficOnce(ctx, []int64{1, 2, 3, 4, 5, 8}, now, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(reset) != 3 {
		t.Fatalf("reset %d rows, want 3", len(reset))
	}
	for _, id := range []int64{1, 2, 3} {
		got := f.load(t, id)
		if got.Upload != 0 || got.Download != 0 || got.Status != usersub.SubscribeStatusActive || got.FinishedAt != nil || got.TrafficResetAt == nil || !got.TrafficResetAt.Equal(day) {
			t.Fatalf("row %d after reset: %+v", id, got)
		}
	}
	for _, id := range []int64{4, 5, 8} {
		if got := f.load(t, id); got.Upload != 5 {
			t.Fatalf("row %d is not due but was reset: %+v", id, got)
		}
	}

	// A repeated run the same day finds nothing to do.
	if err := f.db.Model(&usersub.Subscribe{}).Where("id = 1").Update("upload", 7).Error; err != nil {
		t.Fatal(err)
	}
	again, err := subs.ResetSubscribeTrafficOnce(ctx, []int64{1, 2, 3}, now, day)
	if err != nil || len(again) != 0 {
		t.Fatalf("repeat reset = %d rows, %v", len(again), err)
	}
	if got := f.load(t, 1); got.Upload != 7 {
		t.Fatalf("repeat reset cleared usage again: %+v", got)
	}
}
