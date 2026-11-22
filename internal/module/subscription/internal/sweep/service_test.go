package sweep

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// The sweep finishes the live subscriptions whose traffic is used up
// (Finished) or whose term ended (Expired), notifies their owners and drops
// their cached rows; the rest keep running.
func TestCheckSubscriptionsFinishesExhaustedAndExpiredSubscriptions(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1})
	now := timeutil.Now()
	future, past := now.Add(24*time.Hour), now.Add(-time.Minute)
	exhausted := f.Subscription(t, usersub.Subscribe{UserId: 1, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 60, Download: 40, Status: usersub.SubscribeStatusActive, Token: "exhausted-token"})
	expired := f.Subscription(t, usersub.Subscribe{UserId: 2, SubscribeId: 1, ExpireTime: past, Status: usersub.SubscribeStatusActive})
	legacyPending := f.Subscription(t, usersub.Subscribe{UserId: 3, SubscribeId: 1, ExpireTime: past, Status: usersub.SubscribeStatusPending})
	running := f.Subscription(t, usersub.Subscribe{UserId: 4, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 10, Status: usersub.SubscribeStatusActive})
	unlimited := f.Subscription(t, usersub.Subscribe{UserId: 5, SubscribeId: 1, ExpireTime: usersub.NoLimitExpiry(), Traffic: 0, Upload: 1 << 40, Status: usersub.SubscribeStatusActive})
	stopped := f.Subscription(t, usersub.Subscribe{UserId: 6, SubscribeId: 1, ExpireTime: past, Status: usersub.SubscribeStatusStopped})
	if _, err := f.Store.UserSubscription().FindOneSubscribeByToken(ctx, "exhausted-token"); err != nil {
		t.Fatal(err)
	}

	svc, notifier := newSweepService(f, owners{emails: map[int64]string{1: "one@example.com", 2: "two@example.com"}})
	if err := svc.CheckSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}

	for sub, want := range map[*usersub.Subscribe]uint8{
		exhausted: usersub.SubscribeStatusFinished, expired: usersub.SubscribeStatusExpired, legacyPending: usersub.SubscribeStatusExpired,
		running: usersub.SubscribeStatusActive, unlimited: usersub.SubscribeStatusActive, stopped: usersub.SubscribeStatusStopped,
	} {
		got := f.Load(t, sub.Id)
		if got.Status != want {
			t.Fatalf("subscription of user %d: status %d, want %d", sub.UserId, got.Status, want)
		}
		if finished := want == usersub.SubscribeStatusFinished || (want == usersub.SubscribeStatusExpired); finished != (got.FinishedAt != nil) {
			t.Fatalf("subscription of user %d: finished_at %v", sub.UserId, got.FinishedAt)
		}
	}
	sort.Strings(notifier.expired)
	if strings.Join(notifier.exhausted, ",") != "one@example.com" || strings.Join(notifier.expired, ",") != "two@example.com" {
		t.Fatalf("notices: exhausted %v expired %v", notifier.exhausted, notifier.expired)
	}
	if f.Cached("cache:user:subscribe:token:exhausted-token") {
		t.Fatal("the finished subscription stays cached")
	}

	// The next pass has nothing to do and sends nothing again.
	if err := svc.CheckSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notifier.exhausted) != 1 || len(notifier.expired) != 1 {
		t.Fatalf("a repeated pass notified again: %v %v", notifier.exhausted, notifier.expired)
	}
}

// A subscription renewed or reset after the sweep selected it keeps running:
// the finishing statement checks the expiry and traffic again.
func TestMarkSubscribesFinishedRechecksTheRow(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	now := timeutil.Now()
	renewed := f.Subscription(t, usersub.Subscribe{UserId: 1, SubscribeId: 1, ExpireTime: now.Add(24 * time.Hour), Status: usersub.SubscribeStatusActive})
	reset := f.Subscription(t, usersub.Subscribe{UserId: 2, SubscribeId: 1, ExpireTime: now.Add(24 * time.Hour), Traffic: 100, Upload: 1, Status: usersub.SubscribeStatusActive})
	subs := f.Store.UserSubscription()
	if err := subs.MarkSubscribesFinished(ctx, []int64{renewed.Id}, usersub.SubscribeStatusExpired, now); err != nil {
		t.Fatal(err)
	}
	if err := subs.MarkSubscribesFinished(ctx, []int64{reset.Id}, usersub.SubscribeStatusFinished, now); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*usersub.Subscribe{renewed, reset} {
		if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusActive || got.FinishedAt != nil {
			t.Fatalf("stale sweep result finished a running subscription: %+v", got)
		}
	}
}

// failingStore fails the sweep's queries.
type failingStore struct {
	*subtest.Store
	err error
}

func (s failingStore) InSubscriptionTx(context.Context, func(repository.SubscriptionStore) error) error {
	return s.err
}

// Both sweeps run and both failures come back, named after their sweep, so
// the task records them instead of reporting success.
func TestCheckSubscriptionsReportsEverySweepFailure(t *testing.T) {
	f := subtest.New(t)
	svc, _ := newSweepService(f, owners{})
	failure := errors.New("database unavailable")
	svc.deps.Store = failingStore{Store: f.Store, err: failure}

	err := svc.CheckSubscriptions(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("CheckSubscriptions = %v, want the sweep failure", err)
	}
	for _, sweep := range []string{"[Check Subscription Traffic]", "[Check Subscription Expire]"} {
		if !strings.Contains(err.Error(), sweep) {
			t.Fatalf("error %q does not name the %s sweep", err, sweep)
		}
	}
}
