package quotatask

import (
	"context"
	"errors"
	"testing"
	"time"

	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/internal/repository"
)

// quotaStore is the package's Store over the subscription fixture: the grant
// stage runs in the fixture's transactions against its tables and the task
// bookkeeping records the progress.
type quotaStore struct {
	*subtest.Store
	tasks *quotaTasks
	cache *quotaAccounts
}

var _ Store = (*quotaStore)(nil)

func newQuotaStore(f *subtest.Fixture) *quotaStore {
	return &quotaStore{
		Store: f.Store,
		tasks: &quotaTasks{},
		cache: &quotaAccounts{},
	}
}

// quotaGift is one gift credit the gift stage requested.
type quotaGift struct {
	taskID, subID, userID, amount int64
	at                            time.Time
}

// quotaGifts is the billing port of the gift stage: it records the credits
// requested and reports a (task, subscription) credited once one was.
type quotaGifts struct {
	credited map[string]bool
	credits  []quotaGift
}

var _ GiftLedger = (*quotaGifts)(nil)

func newQuotaGifts() *quotaGifts { return &quotaGifts{credited: map[string]bool{}} }

func (g *quotaGifts) QuotaGiftCredited(_ context.Context, taskID, subID int64) (bool, error) {
	return g.credited[inboxKey(taskID, subID)], nil
}

func (g *quotaGifts) CreditQuotaGift(_ context.Context, taskID, subID, userID, amount int64, at time.Time) error {
	g.credits = append(g.credits, quotaGift{taskID: taskID, subID: subID, userID: userID, amount: amount, at: at})
	g.credited[inboxKey(taskID, subID)] = true
	return nil
}

func (s *quotaStore) InPlatformTx(_ context.Context, fn func(repository.PlatformStore) error) error {
	return fn(quotaPlatform{tasks: s.tasks})
}

func (s *quotaStore) Task() repository.TaskRepo { return s.tasks }

// quotaPlatform is the platform transaction's view: only the task rows.
type quotaPlatform struct {
	tasks *quotaTasks
}

var _ repository.PlatformStore = quotaPlatform{}

func (p quotaPlatform) Task() repository.TaskRepo   { return p.tasks }
func (quotaPlatform) System() repository.SystemRepo { return nil }
func (quotaPlatform) Log() repository.LogRepo       { return nil }
func (quotaPlatform) Inbox() repository.InboxRepo   { return nil }
func (quotaPlatform) Outbox() repository.OutboxRepo { return nil }

// quotaTasks records the progress updates of a task that stays active; the
// run makes no other task writes.
type quotaTasks struct {
	updates []task.Task
}

var (
	_ repository.TaskRepo = (*quotaTasks)(nil)

	errTaskUnsupported = errors.New("not supported by the quota task test store")
)

func (r *quotaTasks) UpdateActiveProgress(_ context.Context, data *task.Task) (bool, error) {
	r.updates = append(r.updates, *data)
	return true, nil
}

func (r *quotaTasks) Insert(context.Context, *task.Task) error { return errTaskUnsupported }
func (r *quotaTasks) FindOne(context.Context, int64) (*task.Task, error) {
	return nil, errTaskUnsupported
}
func (r *quotaTasks) FindOneByType(context.Context, int64, task.Type) (*task.Task, error) {
	return nil, errTaskUnsupported
}
func (r *quotaTasks) QueryTaskList(context.Context, *task.Filter) (int64, []*task.Task, error) {
	return 0, nil, errTaskUnsupported
}
func (r *quotaTasks) Update(context.Context, *task.Task) error { return errTaskUnsupported }
func (r *quotaTasks) UpdateActive(context.Context, *task.Task) (bool, error) {
	return false, errTaskUnsupported
}
func (r *quotaTasks) UpdateActiveProgressWithError(context.Context, *task.Task, *task.TaskError) (bool, error) {
	return false, errTaskUnsupported
}
func (r *quotaTasks) UpdateStatus(context.Context, int64, int8) error { return errTaskUnsupported }
func (r *quotaTasks) UpdateStatusFrom(context.Context, int64, task.Type, []int8, int8) (bool, error) {
	return false, errTaskUnsupported
}
func (r *quotaTasks) UpdateStatusAndErrorFrom(context.Context, int64, task.Type, []int8, int8, string) (bool, error) {
	return false, errTaskUnsupported
}
func (r *quotaTasks) InsertError(context.Context, *task.TaskError) error { return errTaskUnsupported }
func (r *quotaTasks) FindErrors(context.Context, []int64) ([]*task.TaskError, error) {
	return nil, errTaskUnsupported
}

// quotaAccounts is the identity port counting the accounts whose cached
// rows were dropped. It finds no accounts: the gifts under test refresh the
// recipient's cache by id.
type quotaAccounts struct {
	cleared int
}

var _ Accounts = (*quotaAccounts)(nil)

func (a *quotaAccounts) FindUsersByIds(context.Context, []int64) ([]*userEntity.User, error) {
	return nil, nil
}

func (a *quotaAccounts) ClearUserCache(_ context.Context, userIDs ...int64) error {
	a.cleared += len(userIDs)
	return nil
}

// testTaskID is the quota task every test runs.
const testTaskID int64 = 7

// grantMarker reports whether the grant stage of the test task committed its
// marker for the subscription.
func grantMarker(t *testing.T, f *subtest.Fixture, subID int64) bool {
	t.Helper()
	record, err := f.Store.Inbox().Find(context.Background(), inboxQuotaGrant, inboxKey(testTaskID, subID))
	if err != nil {
		t.Fatal(err)
	}
	return record != nil
}

// A write that fails rolls the grant back whole: no marker records it, so the
// retry applies it.
func TestGrantSubscriptionDoesNotMarkInboxAfterUpdateFailure(t *testing.T) {
	f := subtest.New(t)
	term := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	sub := f.Subscription(t, usersub.Subscribe{Id: 9, UserId: 3, ExpireTime: term, Status: usersub.SubscribeStatusActive})
	if err := f.DB.Exec(`CREATE TRIGGER fail_update BEFORE UPDATE ON user_subscribe BEGIN SELECT RAISE(ABORT, 'write failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(Deps{Store: newQuotaStore(f)})

	stale := *sub
	if err := svc.grantSubscription(context.Background(), 7, &stale, task.QuotaContent{Days: 1}, time.Now()); err == nil {
		t.Fatal("the failed write was reported as granted")
	}
	if grantMarker(t, f, sub.Id) {
		t.Fatal("the rolled-back grant left its marker")
	}
	if got := f.Load(t, sub.Id); !got.ExpireTime.Equal(term) {
		t.Fatalf("expire_time = %v, want the untouched %v", got.ExpireTime, term)
	}

	if err := f.DB.Exec(`DROP TRIGGER fail_update`).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.grantSubscription(context.Background(), 7, &stale, task.QuotaContent{Days: 1}, time.Now()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := f.Load(t, sub.Id); !got.ExpireTime.Equal(term.AddDate(0, 0, 1)) || !grantMarker(t, f, sub.Id) {
		t.Fatalf("retry stored %+v", got)
	}
}

// A traffic reset follows the rule of every traffic reset: the exhausted
// subscription inside its term is active again, its usage cleared and the
// reset logged. The caller's copy is refreshed to the stored row.
func TestGrantSubscriptionReactivatesTrafficFinishedSubscription(t *testing.T) {
	f := subtest.New(t)
	finishedAt := time.Now().Add(-time.Minute)
	sub := f.Subscription(t, usersub.Subscribe{
		Id: 9, UserId: 3, Status: usersub.SubscribeStatusFinished, FinishedAt: &finishedAt,
		ExpireTime: time.Now().Add(24 * time.Hour), Traffic: 30, Download: 10, Upload: 20,
	})
	svc := NewService(Deps{Store: newQuotaStore(f)})

	stale := *sub
	if err := svc.grantSubscription(context.Background(), 7, &stale, task.QuotaContent{ResetTraffic: true}, time.Now()); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	stored := f.Load(t, sub.Id)
	if stored.Status != usersub.SubscribeStatusActive || stored.FinishedAt != nil || stored.Download != 0 || stored.Upload != 0 || !grantMarker(t, f, sub.Id) {
		t.Fatalf("reset quota did not reactivate the subscription atomically: %+v", stored)
	}
	if rows := f.Logs(t, log.TypeResetSubscribe); len(rows) != 1 || rows[0].ObjectID != sub.Id {
		t.Fatalf("reset logs = %+v", rows)
	}
	if stale.Status != stored.Status || stale.FinishedAt != nil || stale.Download != 0 || stale.Upload != 0 {
		t.Fatalf("caller copy was not refreshed: %+v", stale)
	}
}

// Finite days never downgrade an unlimited subscription to a finite term.
func TestGrantSubscriptionPreservesNoLimitExpiry(t *testing.T) {
	f := subtest.New(t)
	sub := f.Subscription(t, usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusActive, ExpireTime: usersub.NoLimitExpiry()})
	svc := NewService(Deps{Store: newQuotaStore(f)})

	stale := *sub
	if err := svc.grantSubscription(context.Background(), 7, &stale, task.QuotaContent{Days: 30}, time.Now()); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	if stored := f.Load(t, sub.Id); !usersub.NoExpiry(stored.ExpireTime) || stored.Status != usersub.SubscribeStatusActive || !grantMarker(t, f, sub.Id) {
		t.Fatalf("the unlimited subscription was downgraded: %+v", stored)
	}
}

// The gift stage hands billing the plan share for the subscription's owner,
// dated at the task run, and refreshes the owner's cached balance.
func TestGrantGiftCreditsThePlanShare(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 13, UnitPrice: 1990})
	store, gifts := newQuotaStore(f), newQuotaGifts()
	svc := NewService(Deps{Accounts: store.cache, Store: store, Gifts: gifts})
	now := time.Now()

	err := svc.grantGift(context.Background(), 7, &usersub.Subscribe{Id: 9, UserId: 11, SubscribeId: 13}, task.QuotaContent{GiftType: 2, GiftValue: 10}, now)
	if err != nil {
		t.Fatalf("grantGift: %v", err)
	}
	want := quotaGift{taskID: 7, subID: 9, userID: 11, amount: 199, at: now}
	if len(gifts.credits) != 1 || gifts.credits[0] != want || store.cache.cleared != 1 {
		t.Fatalf("gift credits = %+v, cache clears = %d; want %+v and one clear", gifts.credits, store.cache.cleared, want)
	}
}

// A gift stage that already committed is not granted again: no plan lookup
// (the plan is gone), no billing credit, but the cached balance is
// refreshed.
func TestGrantGiftReplaySkipsPlanLookupAndRefreshesCache(t *testing.T) {
	f := subtest.New(t)
	gifts := newQuotaGifts()
	gifts.credited[inboxKey(7, 9)] = true
	store := newQuotaStore(f)
	svc := NewService(Deps{Accounts: store.cache, Store: store, Gifts: gifts})

	err := svc.grantGift(context.Background(), 7, &usersub.Subscribe{Id: 9, UserId: 11, SubscribeId: 13}, task.QuotaContent{GiftType: 2, GiftValue: 10}, time.Now())
	if err != nil {
		t.Fatalf("completed gift replay: %v", err)
	}
	if len(gifts.credits) != 0 || store.cache.cleared != 1 {
		t.Fatalf("completed gift stage replayed work: credits=%+v cache_clears=%d", gifts.credits, store.cache.cleared)
	}
}

func TestValidateContentRejectsInvalidQuotaActions(t *testing.T) {
	for _, content := range []task.QuotaContent{
		{},
		{GiftType: 1},
		{GiftType: 9, GiftValue: 1},
		{GiftType: 1, GiftValue: ^uint64(0)},
	} {
		if err := validateContent(content); err == nil {
			t.Fatalf("invalid content accepted: %+v", content)
		}
	}
	if err := validateContent(task.QuotaContent{Days: 1}); err != nil {
		t.Fatalf("valid quota action rejected: %v", err)
	}
}
