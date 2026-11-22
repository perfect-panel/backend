package trafficreset

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
)

func dayStart(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func resetFor(day time.Time) *time.Time { return &day }

// A run missed on the reset day is made good by the next one. On June 3rd,
// with the 1st's run skipped: a subscription last reset for May 1st is owed
// June 1st's reset, one already reset for June 1st is not, one whose monthly
// day has not come yet is not, and two missed cycles are made good by one
// reset. A subscription never reset yet keeps the reset-day rule: the marker
// is newer than the resets, and reading "never" as "missed" would clear every
// row written before it once.
func TestResetDueCatchesUpAResetDayTheRunMissed(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, ResetCycle: int64(period.CycleFirstOfMonth), Traffic: 100})
	f.Plan(t, subscribe.Subscribe{Id: 2, ResetCycle: int64(period.CycleMonthly), Traffic: 100})
	f.Plan(t, subscribe.Subscribe{Id: 3, ResetCycle: int64(period.CycleYearly), Traffic: 100})
	now := time.Date(2026, 6, 3, 0, 30, 0, 0, time.UTC)
	term := now.AddDate(0, 6, 0)
	user := int64(0)
	sub := func(planID int64, start time.Time, resetAt *time.Time, status uint8, expire time.Time) *usersub.Subscribe {
		user++
		row := usersub.Subscribe{UserId: user, SubscribeId: planID, StartTime: start, ExpireTime: expire, Traffic: 100, Upload: 60, Download: 40, Status: status, TrafficResetAt: resetAt}
		if status == usersub.SubscribeStatusFinished {
			finished := now.Add(-time.Hour)
			row.FinishedAt = &finished
		}
		return f.Subscription(t, row)
	}
	march := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	missedFirst := sub(1, march, resetFor(dayStart(2026, 5, 1)), usersub.SubscribeStatusActive, term)
	doneFirst := sub(1, march, resetFor(dayStart(2026, 6, 1)), usersub.SubscribeStatusActive, term)
	neverFirst := sub(1, march, nil, usersub.SubscribeStatusActive, term)
	twiceMissed := sub(1, march, resetFor(dayStart(2026, 4, 1)), usersub.SubscribeStatusActive, term)
	missedExhausted := sub(1, march, resetFor(dayStart(2026, 5, 1)), usersub.SubscribeStatusFinished, term)
	expiredMissed := sub(1, march, resetFor(dayStart(2026, 5, 1)), usersub.SubscribeStatusActive, now.Add(-time.Hour))
	missedMonthly := sub(2, march, resetFor(dayStart(2026, 5, 1)), usersub.SubscribeStatusActive, term)
	monthlyNotYet := sub(2, time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC), resetFor(dayStart(2026, 5, 5)), usersub.SubscribeStatusActive, term)
	missedYearly := sub(3, time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC), resetFor(dayStart(2025, 6, 1)), usersub.SubscribeStatusActive, term)

	svc := newResetService(f, now, time.UTC, 0)
	if err := svc.ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	reset := map[*usersub.Subscribe]bool{
		missedFirst: true, twiceMissed: true, missedExhausted: true, missedMonthly: true, missedYearly: true,
		doneFirst: false, neverFirst: false, expiredMissed: false, monthlyNotYet: false,
	}
	for row, want := range reset {
		got := f.Load(t, row.Id)
		if cleared := got.Upload == 0 && got.Download == 0; cleared != want {
			t.Fatalf("subscription %d (plan %d, marker %v) reset = %v, want %v", row.Id, row.SubscribeId, row.TrafficResetAt, cleared, want)
		}
		if want && (got.TrafficResetAt == nil || !got.TrafficResetAt.Equal(dayStart(2026, 6, 3))) {
			t.Fatalf("subscription %d is not marked reset for the day it caught up: %+v", row.Id, got)
		}
	}
	if got := f.Load(t, missedExhausted.Id); got.Status != usersub.SubscribeStatusActive || got.FinishedAt != nil {
		t.Fatalf("the caught-up reset did not reactivate the exhausted subscription: %+v", got)
	}
	if logs := f.Logs(t, log.TypeResetSubscribe); len(logs) != 5 {
		t.Fatalf("audit rows = %d, want one per caught-up subscription", len(logs))
	}

	// Caught up, the subscriptions are not reset again today, and the next
	// cycle resets them on its day.
	if err := f.DB.Model(&usersub.Subscribe{}).Where("id IN ?", []int64{missedFirst.Id, missedMonthly.Id}).Update("upload", 7).Error; err != nil {
		t.Fatal(err)
	}
	if err := newResetService(f, now.Add(2*time.Hour), time.UTC, 0).ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	for _, row := range []*usersub.Subscribe{missedFirst, missedMonthly} {
		if got := f.Load(t, row.Id); got.Upload != 7 {
			t.Fatalf("a repeated run reset the caught-up subscription %d again: %+v", row.Id, got)
		}
	}
	if err := newResetService(f, time.Date(2026, 7, 1, 0, 30, 0, 0, time.UTC), time.UTC, 0).ResetDue(ctx); err != nil {
		t.Fatal(err)
	}
	// No run happened between June 3rd and July 1st, so July 1st also makes
	// good the June 5th reset of the subscription whose monthly day is the
	// 5th.
	for _, row := range []*usersub.Subscribe{missedFirst, missedMonthly, doneFirst, neverFirst, monthlyNotYet} {
		if got := f.Load(t, row.Id); got.Upload != 0 {
			t.Fatalf("the next cycle did not reset subscription %d: %+v", row.Id, got)
		}
	}
}

// A day the reset never ran on, in the middle of otherwise regular runs: the
// run of the day after resets what the skipped day owed and nothing more.
func TestResetDueAfterASkippedDay(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, ResetCycle: int64(period.CycleFirstOfMonth), Traffic: 100})
	f.Plan(t, subscribe.Subscribe{Id: 2, ResetCycle: int64(period.CycleMonthly), Traffic: 100})
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	term := start.AddDate(1, 0, 0)
	first := f.Subscription(t, usersub.Subscribe{UserId: 1, SubscribeId: 1, StartTime: start, ExpireTime: term, Traffic: 100, Upload: 60, Status: usersub.SubscribeStatusActive})
	monthly := f.Subscription(t, usersub.Subscribe{UserId: 2, SubscribeId: 2, StartTime: start, ExpireTime: term, Traffic: 100, Upload: 60, Status: usersub.SubscribeStatusActive})
	other := f.Subscription(t, usersub.Subscribe{UserId: 3, SubscribeId: 2, StartTime: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), ExpireTime: term, Traffic: 100, Upload: 60, Status: usersub.SubscribeStatusActive})

	run := func(day time.Time) {
		if err := newResetService(f, day.Add(30*time.Minute), time.UTC, 0).ResetDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	use := func(rows ...*usersub.Subscribe) {
		for _, row := range rows {
			if err := f.DB.Model(&usersub.Subscribe{}).Where("id = ?", row.Id).Update("upload", 9).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// June 1st runs and resets both; July 1st is skipped; July 2nd runs.
	run(dayStart(2026, 6, 1))
	for _, row := range []*usersub.Subscribe{first, monthly} {
		if got := f.Load(t, row.Id); got.Upload != 0 {
			t.Fatalf("June 1st did not reset subscription %d: %+v", row.Id, got)
		}
	}
	use(first, monthly, other)
	run(dayStart(2026, 7, 2))
	for _, row := range []*usersub.Subscribe{first, monthly} {
		if got := f.Load(t, row.Id); got.Upload != 0 {
			t.Fatalf("July 2nd did not make good the skipped 1st for subscription %d: %+v", row.Id, got)
		}
	}
	// The subscription whose day is the 2nd is reset on its own day, once.
	if got := f.Load(t, other.Id); got.Upload != 0 {
		t.Fatalf("July 2nd did not reset the subscription whose day it is: %+v", got)
	}
	use(first, monthly, other)
	run(dayStart(2026, 7, 3))
	for _, row := range []*usersub.Subscribe{first, monthly, other} {
		if got := f.Load(t, row.Id); got.Upload != 9 {
			t.Fatalf("July 3rd reset subscription %d again: %+v", row.Id, got)
		}
	}
	perSub := map[int64]int{}
	for _, row := range f.Logs(t, log.TypeResetSubscribe) {
		perSub[row.ObjectID]++
	}
	if perSub[first.Id] != 2 || perSub[monthly.Id] != 2 || perSub[other.Id] != 1 {
		t.Fatalf("audit rows per subscription = %v, want 2, 2 and 1", perSub)
	}
}
