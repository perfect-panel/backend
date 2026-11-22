package sweep

import (
	"context"
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

// afterSelection is a write landing between the sweep's selection and its
// finishing statement, as a payment's renewal or a traffic reset does.
type afterSelection func(ctx context.Context, subs repository.UserSubscriptionRepo, selected []*usersub.Subscribe)

// interleavingStore runs the sweep's transaction with afterSelection applied
// to every selection the sweep makes.
type interleavingStore struct {
	*subtest.Store
	after afterSelection
}

func (s interleavingStore) InSubscriptionTx(ctx context.Context, fn func(repository.SubscriptionStore) error) error {
	return s.Store.InSubscriptionTx(ctx, func(inner repository.SubscriptionStore) error {
		return fn(interleavedStore{SubscriptionStore: inner, after: s.after})
	})
}

type interleavedStore struct {
	repository.SubscriptionStore
	after afterSelection
}

func (s interleavedStore) UserSubscription() repository.UserSubscriptionRepo {
	return interleavedSubs{UserSubscriptionRepo: s.SubscriptionStore.UserSubscription(), after: s.after}
}

type interleavedSubs struct {
	repository.UserSubscriptionRepo
	after afterSelection
}

func (r interleavedSubs) FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*usersub.Subscribe, error) {
	list, err := r.UserSubscriptionRepo.FindExpiredSubscribes(ctx, now)
	if err == nil {
		r.after(ctx, r.UserSubscriptionRepo, list)
	}
	return list, err
}

func (r interleavedSubs) FindTrafficExceededSubscribes(ctx context.Context) ([]*usersub.Subscribe, error) {
	list, err := r.UserSubscriptionRepo.FindTrafficExceededSubscribes(ctx)
	if err == nil {
		r.after(ctx, r.UserSubscriptionRepo, list)
	}
	return list, err
}

// A subscription renewed or reset after the sweep selected it and before it
// finished it keeps running, and its owner is not told of an expiry or an
// exhaustion that did not happen: only the subscriptions the finishing
// statement flipped are notified.
func TestCheckSubscriptionsNotifiesOnlyTheSubscriptionsItFinished(t *testing.T) {
	f := subtest.New(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1})
	now := timeutil.Now()
	future, past := now.Add(24*time.Hour), now.Add(-time.Minute)
	expired := f.Subscription(t, usersub.Subscribe{UserId: 1, SubscribeId: 1, ExpireTime: past, Status: usersub.SubscribeStatusActive})
	renewed := f.Subscription(t, usersub.Subscribe{UserId: 2, SubscribeId: 1, ExpireTime: past, Status: usersub.SubscribeStatusActive})
	exhausted := f.Subscription(t, usersub.Subscribe{UserId: 3, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 100, Status: usersub.SubscribeStatusActive})
	reset := f.Subscription(t, usersub.Subscribe{UserId: 4, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 100, Status: usersub.SubscribeStatusActive})

	svc, notifier := newSweepService(f, owners{emails: map[int64]string{1: "one@example.com", 2: "two@example.com", 3: "three@example.com", 4: "four@example.com"}})
	svc.deps.Store = interleavingStore{Store: f.Store, after: func(ctx context.Context, subs repository.UserSubscriptionRepo, selected []*usersub.Subscribe) {
		for _, sub := range selected {
			switch sub.Id {
			case renewed.Id:
				sub.ExpireTime = future
				if err := subs.UpdateSubscribeColumns(ctx, sub, "expire_time"); err != nil {
					t.Fatal(err)
				}
			case reset.Id:
				sub.Upload, sub.Download = 0, 0
				if err := subs.UpdateSubscribeColumns(ctx, sub, "upload", "download"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}}
	if err := svc.CheckSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}

	for sub, want := range map[*usersub.Subscribe]uint8{
		expired: usersub.SubscribeStatusExpired, exhausted: usersub.SubscribeStatusFinished,
		renewed: usersub.SubscribeStatusActive, reset: usersub.SubscribeStatusActive,
	} {
		if got := f.Load(t, sub.Id); got.Status != want {
			t.Fatalf("subscription of user %d: status %d, want %d", sub.UserId, got.Status, want)
		}
	}
	sort.Strings(notifier.expired)
	sort.Strings(notifier.exhausted)
	if strings.Join(notifier.expired, ",") != "one@example.com" || strings.Join(notifier.exhausted, ",") != "three@example.com" {
		t.Fatalf("notices: expired %v exhausted %v; want only the finished subscriptions' owners", notifier.expired, notifier.exhausted)
	}
}
