package quotatask

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// The task reads its targets once when it starts. A grant applied later must
// not write that snapshot back over traffic usage, credentials or an admin
// hold that changed in between.
func TestGrantSubscriptionKeepsConcurrentWritesAndAdminHold(t *testing.T) {
	f := subtest.New(t)
	now := timeutil.Now().Truncate(time.Millisecond)
	term := now.Add(24 * time.Hour)
	f.Subscription(t, usersub.Subscribe{
		Id: 9, UserId: 3, SubscribeId: 1, Status: usersub.SubscribeStatusStopped, ExpireTime: term,
		Traffic: 1000, Upload: 500, Download: 700, Token: "token-fresh", UUID: "uuid-fresh",
	})
	stale := &usersub.Subscribe{
		Id: 9, UserId: 3, SubscribeId: 1, Status: usersub.SubscribeStatusActive, ExpireTime: term,
		Traffic: 1000, Token: "token-stale", UUID: "uuid-stale",
	}
	svc := NewService(Deps{Store: newQuotaStore(f)})

	if err := svc.grantSubscription(context.Background(), 7, stale, task.QuotaContent{Days: 30}, now); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	row := f.Load(t, 9)
	if row.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("grant lifted the admin hold: status=%d", row.Status)
	}
	if row.Upload != 500 || row.Download != 700 || row.Token != "token-fresh" || row.UUID != "uuid-fresh" {
		t.Fatalf("grant overwrote concurrent writes with its snapshot: %+v", row)
	}
	if !row.ExpireTime.Equal(term.AddDate(0, 0, 30)) {
		t.Fatalf("expire_time = %v, want %v", row.ExpireTime, term.AddDate(0, 0, 30))
	}
	if stale.Token != "token-fresh" || stale.Status != usersub.SubscribeStatusStopped || !grantMarker(t, f, 9) {
		t.Fatalf("caller copy not refreshed or marker missing: sub=%+v", stale)
	}
}

func TestGrantSubscriptionResetKeepsAdminHold(t *testing.T) {
	f := subtest.New(t)
	now := timeutil.Now().Truncate(time.Millisecond)
	f.Subscription(t, usersub.Subscribe{
		Id: 9, UserId: 3, Status: usersub.SubscribeStatusStopped, ExpireTime: now.Add(time.Hour),
		Traffic: 1000, Upload: 500, Download: 700,
	})
	stale := &usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusFinished}
	svc := NewService(Deps{Store: newQuotaStore(f)})

	if err := svc.grantSubscription(context.Background(), 7, stale, task.QuotaContent{ResetTraffic: true}, now); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	if row := f.Load(t, 9); row.Status != usersub.SubscribeStatusStopped || row.Upload != 0 || row.Download != 0 {
		t.Fatalf("reset must clear usage and keep the hold: %+v", row)
	}
}

func TestGrantSubscriptionSkipsRowDeductedAfterTaskStart(t *testing.T) {
	f := subtest.New(t)
	now := timeutil.Now().Truncate(time.Millisecond)
	term := now.Add(time.Hour)
	f.Subscription(t, usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusDeducted, ExpireTime: term})
	stale := &usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusActive, ExpireTime: term}
	svc := NewService(Deps{Store: newQuotaStore(f)})

	err := svc.grantSubscription(context.Background(), 7, stale, task.QuotaContent{Days: 30}, now)
	if !errors.Is(err, errQuotaIneligible) {
		t.Fatalf("grantSubscription error = %v, want errQuotaIneligible", err)
	}
	if row := f.Load(t, 9); row.Status != usersub.SubscribeStatusDeducted || !row.ExpireTime.Equal(term) || grantMarker(t, f, 9) {
		t.Fatalf("deducted row was granted: %+v", row)
	}
}

func TestGrantSubscriptionReactivatesExpiredSubscription(t *testing.T) {
	f := subtest.New(t)
	now := timeutil.Now().Truncate(time.Millisecond)
	finishedAt := now.Add(-time.Hour)
	f.Subscription(t, usersub.Subscribe{
		Id: 9, UserId: 3, Status: usersub.SubscribeStatusExpired, ExpireTime: now.Add(-2 * time.Hour), FinishedAt: &finishedAt,
	})
	stale := &usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusExpired}
	svc := NewService(Deps{Store: newQuotaStore(f)})

	if err := svc.grantSubscription(context.Background(), 7, stale, task.QuotaContent{Days: 10}, now); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	if row := f.Load(t, 9); row.Status != usersub.SubscribeStatusActive || row.FinishedAt != nil || !row.ExpireTime.Equal(now.AddDate(0, 0, 10)) {
		t.Fatalf("expired subscription was not extended from now and reactivated: %+v", row)
	}
}

// A subscription deducted after the task read its targets is reported like
// one deducted up front: no time grant and no gift.
func TestProcessSubscribesReportsRowDeductedMidTask(t *testing.T) {
	f := subtest.New(t)
	now := timeutil.Now().Truncate(time.Millisecond)
	f.Subscription(t, usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusDeducted, ExpireTime: now.Add(time.Hour)})
	stale := &usersub.Subscribe{Id: 9, UserId: 3, Status: usersub.SubscribeStatusActive, ExpireTime: now.Add(time.Hour)}
	store, gifts := newQuotaStore(f), newQuotaGifts()
	svc := NewService(Deps{Store: store, Gifts: gifts})
	taskInfo := &task.Task{Id: 7, Status: task.StatusInProgress, Total: 1}

	content := task.QuotaContent{Days: 1, GiftType: 1, GiftValue: 100}
	if err := svc.processSubscribes(context.Background(), []*usersub.Subscribe{stale}, content, taskInfo); err != nil {
		t.Fatalf("processSubscribes: %v", err)
	}
	if len(gifts.credits) != 0 {
		t.Fatalf("gift granted to a deducted subscription: credits=%+v", gifts.credits)
	}
	if taskInfo.Current != 1 || !strings.Contains(taskInfo.Errors, "deducted") {
		t.Fatalf("deducted subscription not reported: current=%d errors=%q", taskInfo.Current, taskInfo.Errors)
	}
	if updates := store.tasks.updates; len(updates) == 0 || updates[len(updates)-1].Status != task.StatusFailed {
		t.Fatalf("task updates = %+v, want the run recorded as failed", updates)
	}
	if row := f.Load(t, 9); row.Status != usersub.SubscribeStatusDeducted {
		t.Fatalf("deducted row changed: %+v", row)
	}
}
