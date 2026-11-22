package trafficreset

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
)

func newResetService(f *subtest.Fixture, now time.Time, loc *time.Location, batchSize int) *Service {
	cal := period.In(loc)
	return NewService(Deps{
		Store:     f.Store,
		Plans:     f.Store.Subscribe(),
		Now:       func() time.Time { return now },
		Calendar:  &cal,
		BatchSize: batchSize,
	})
}

// resetFixture holds plans of every cycle and subscriptions due or not due
// on June 1st 2026, 00:30 UTC.
type resetFixture struct {
	*subtest.Fixture
	now                                        time.Time
	firstOfMonth, monthlyDue, yearlyDue, noDay *usersub.Subscribe
	monthlyNotDue, noCycle, expired, stopped   *usersub.Subscribe
	lastDayOfMonth                             *usersub.Subscribe
}

func newResetFixture(t *testing.T) *resetFixture {
	f := &resetFixture{Fixture: subtest.New(t), now: time.Date(2026, 6, 1, 0, 30, 0, 0, time.UTC)}
	for id, cycle := range map[int64]period.Cycle{1: period.CycleFirstOfMonth, 2: period.CycleMonthly, 3: period.CycleYearly, 4: period.CycleNone} {
		f.Plan(t, subscribe.Subscribe{Id: id, ResetCycle: int64(cycle), Traffic: 100})
	}
	term := f.now.AddDate(0, 3, 0)
	used := func(planID int64, start time.Time, status uint8, expire time.Time) *usersub.Subscribe {
		finished := start
		sub := usersub.Subscribe{UserId: planID * 10, SubscribeId: planID, StartTime: start, ExpireTime: expire, Traffic: 100, Upload: 60, Download: 40, Status: status}
		if status == usersub.SubscribeStatusFinished {
			sub.FinishedAt = &finished
		}
		return f.Subscription(t, sub)
	}
	f.firstOfMonth = used(1, time.Date(2026, 3, 17, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusFinished, term)
	f.monthlyDue = used(2, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, term)
	f.monthlyNotDue = used(2, time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, term)
	f.yearlyDue = used(3, time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, usersub.NoLimitExpiry())
	f.noCycle = used(4, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, term)
	f.expired = used(1, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusFinished, f.now.Add(-time.Hour))
	f.stopped = used(1, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusStopped, term)
	f.noDay = used(3, time.Date(2025, 7, 1, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, term)
	f.lastDayOfMonth = used(2, time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC), usersub.SubscribeStatusActive, term)
	return f
}

func (f *resetFixture) assertReset(t *testing.T, want map[*usersub.Subscribe]bool) {
	t.Helper()
	for sub, reset := range want {
		got := f.Load(t, sub.Id)
		if reset && (got.Upload != 0 || got.Download != 0) {
			t.Fatalf("subscription %d (plan %d) was not reset: %+v", sub.Id, sub.SubscribeId, got)
		}
		if !reset && got.Upload == 0 {
			t.Fatalf("subscription %d (plan %d) was reset: %+v", sub.Id, sub.SubscribeId, got)
		}
	}
}

func TestResetDueClearsTrafficOnEachCyclesDay(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	if err := newResetService(f.Fixture, f.now, time.UTC, 0).ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.assertReset(t, map[*usersub.Subscribe]bool{
		f.firstOfMonth: true, f.monthlyDue: true, f.yearlyDue: true,
		f.monthlyNotDue: false, f.noCycle: false, f.expired: false, f.stopped: false, f.noDay: false,
		// Jan 31 resets on the 31st, which June does not have: May 31 was its day.
		f.lastDayOfMonth: false,
	})
	if got := f.Load(t, f.firstOfMonth.Id); got.Status != usersub.SubscribeStatusActive || got.FinishedAt != nil {
		t.Fatalf("the reset did not reactivate the exhausted subscription: %+v", got)
	}
	if got := f.Load(t, f.expired.Id); got.Status != usersub.SubscribeStatusFinished {
		t.Fatalf("the reset brought back an expired subscription: %+v", got)
	}
	logs := f.Logs(t, log.TypeResetSubscribe)
	if len(logs) != 3 {
		t.Fatalf("audit rows = %d, want one per reset", len(logs))
	}
	for _, row := range logs {
		var content log.ResetSubscribe
		if err := content.Unmarshal([]byte(row.Content)); err != nil || content.Type != log.ResetSubscribeTypeAuto || content.UserId == 0 {
			t.Fatalf("audit row %+v: %+v, %v", row, content, err)
		}
	}
}

// A run repeated the same day resets nothing again: not the counters the
// subscriptions used since, and not the audit rows.
func TestResetDueRunsOncePerDay(t *testing.T) {
	f := newResetFixture(t)
	ctx := context.Background()
	svc := newResetService(f.Fixture, f.now, time.UTC, 0)
	if err := svc.ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Model(&usersub.Subscribe{}).Where("id IN ?", []int64{f.firstOfMonth.Id, f.monthlyDue.Id, f.yearlyDue.Id}).Update("upload", 7).Error; err != nil {
		t.Fatal(err)
	}
	later := newResetService(f.Fixture, f.now.Add(90*time.Minute), time.UTC, 0)
	if err := later.ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*usersub.Subscribe{f.firstOfMonth, f.monthlyDue, f.yearlyDue} {
		if got := f.Load(t, sub.Id); got.Upload != 7 {
			t.Fatalf("the repeated run reset subscription %d again: %+v", sub.Id, got)
		}
	}
	if logs := f.Logs(t, log.TypeResetSubscribe); len(logs) != 3 {
		t.Fatalf("audit rows after the repeat = %d, want 3", len(logs))
	}

	// The next month's 1st resets again.
	nextMonth := newResetService(f.Fixture, time.Date(2026, 7, 1, 0, 30, 0, 0, time.UTC), time.UTC, 0)
	if err := nextMonth.ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, f.firstOfMonth.Id); got.Upload != 0 {
		t.Fatalf("the next cycle did not reset: %+v", got)
	}
}

// A run that fails part way, after some batches committed, is retried: the
// retry resets what the failed run did not, and never a subscription twice.
func TestResetDueRetryAfterPartialFailureResetsEachSubscriptionOnce(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 15, 0, 30, 0, 0, time.UTC)
	f.Plan(t, subscribe.Subscribe{Id: 2, ResetCycle: int64(period.CycleMonthly)})
	var subs []*usersub.Subscribe
	for i := 0; i < 4; i++ {
		subs = append(subs, f.Subscription(t, usersub.Subscribe{
			UserId: int64(100 + i), SubscribeId: 2, StartTime: time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC),
			ExpireTime: now.AddDate(0, 6, 0), Upload: 10, Status: usersub.SubscribeStatusActive,
		}))
	}
	// One subscription per batch: the first two batches commit, the third
	// fails as if the connection dropped at commit.
	f.Store.FailCommitsAfter(2, 1)
	svc := newResetService(f, now, time.UTC, 1)
	err := svc.ResetDue(ctx)
	if !errors.Is(err, subtest.ErrInjectedRollback) {
		t.Fatalf("partial run error = %v, want the injected rollback", err)
	}
	for i, sub := range subs {
		got := f.Load(t, sub.Id)
		if reset := got.Upload == 0; reset != (i < 2) {
			t.Fatalf("after the failed run subscription %d reset = %v", i, reset)
		}
	}
	// Usage accrues on a reset subscription before the retry.
	if err := f.DB.Model(&usersub.Subscribe{}).Where("id = ?", subs[0].Id).Update("upload", 3).Error; err != nil {
		t.Fatal(err)
	}

	if err := newResetService(f, now.Add(30*time.Minute), time.UTC, 1).ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, subs[0].Id); got.Upload != 3 {
		t.Fatalf("the retry reset a subscription the failed run had reset: %+v", got)
	}
	for _, sub := range subs[1:] {
		if got := f.Load(t, sub.Id); got.Upload != 0 {
			t.Fatalf("the retry did not reset subscription %d: %+v", sub.Id, got)
		}
	}
	perSub := map[int64]int{}
	for _, row := range f.Logs(t, log.TypeResetSubscribe) {
		perSub[row.ObjectID]++
	}
	for _, sub := range subs {
		if perSub[sub.Id] != 1 {
			t.Fatalf("audit rows per subscription = %v, want exactly one each", perSub)
		}
	}
}

// The reset days are the calendar's: at 00:30 on June 1st in Shanghai it is
// still May 31st in UTC, and a UTC-started subscription's day is read in
// Shanghai.
func TestResetDueUsesTheCalendarZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	f := subtest.New(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, ResetCycle: int64(period.CycleFirstOfMonth)})
	f.Plan(t, subscribe.Subscribe{Id: 2, ResetCycle: int64(period.CycleMonthly)})
	now := time.Date(2026, 6, 1, 0, 30, 0, 0, shanghai)
	term := now.AddDate(0, 3, 0)
	first := f.Subscription(t, usersub.Subscribe{UserId: 1, SubscribeId: 1, StartTime: now.AddDate(0, -1, 0), ExpireTime: term, Upload: 5, Status: usersub.SubscribeStatusActive})
	// 18:00 UTC on Jan 31 is Feb 1 in Shanghai: it resets on the 1st there.
	monthly := f.Subscription(t, usersub.Subscribe{UserId: 2, SubscribeId: 2, StartTime: time.Date(2026, 1, 31, 18, 0, 0, 0, time.UTC), ExpireTime: term, Upload: 5, Status: usersub.SubscribeStatusActive})

	if err := newResetService(f, now, shanghai, 0).ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*usersub.Subscribe{first, monthly} {
		if got := f.Load(t, sub.Id); got.Upload != 0 {
			t.Fatalf("subscription %d not reset on June 1st in Shanghai: %+v", sub.Id, got)
		}
	}
}
