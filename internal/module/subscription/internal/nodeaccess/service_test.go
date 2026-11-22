package nodeaccess

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

func newService(f *subtest.Fixture) *Service {
	return NewService(Deps{Plans: f.Store.Subscribe(), Subscriptions: f.Store.UserSubscription()})
}

// A node scope serves the servable subscriptions of the plans selecting
// any of its nodes or tags, by plan and id, each with the plan whose limits
// the node applies.
func TestServableByNodeScopePairsTheSubscriptionsWithTheirPlans(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "11", SpeedLimit: 100})
	f.Plan(t, subscribe.Subscribe{Id: 2, NodeTags: "hk,us", DeviceLimit: 3})
	f.Plan(t, subscribe.Subscribe{Id: 3, Nodes: "99"})
	future, past := time.Now().Add(24*time.Hour), time.Now().Add(-time.Hour)
	tagged := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 2, Status: usersub.SubscribeStatusActive, ExpireTime: future})
	listed := f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 1, Status: usersub.SubscribeStatusActive, ExpireTime: future})
	f.Subscription(t, usersub.Subscribe{UserId: 9, SubscribeId: 1, Status: usersub.SubscribeStatusExpired, ExpireTime: past})
	f.Subscription(t, usersub.Subscribe{UserId: 10, SubscribeId: 3, Status: usersub.SubscribeStatusActive, ExpireTime: future})

	svc := newService(f)
	served, err := svc.ServableByNodeScope(context.Background(), []int64{11}, []string{"hk"})
	if err != nil {
		t.Fatalf("ServableByNodeScope() error = %v", err)
	}
	if len(served) != 2 ||
		served[0].Subscription.Id != listed.Id || served[0].Plan.Id != 1 || served[0].Plan.SpeedLimit != 100 ||
		served[1].Subscription.Id != tagged.Id || served[1].Plan.Id != 2 || served[1].Plan.DeviceLimit != 3 {
		t.Fatalf("served = %+v", served)
	}

	served, err = svc.ServableByNodeScope(context.Background(), nil, nil)
	if err != nil || len(served) != 0 {
		t.Fatalf("an empty scope served %+v, %v", served, err)
	}
}

// Clearing an account's access drops the cached entries of all its
// subscriptions, whatever their status, leaves the other accounts' entries,
// and reports the node scope of the plans: explicit ids (a damaged list is
// left out) and tags.
func TestClearUserCachesDropsTheCachedSubscriptionsAndReportsTheirScope(t *testing.T) {
	logtest.Discard(t)
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5,6", NodeTags: "hk"})
	f.Plan(t, subscribe.Subscribe{Id: 2, Nodes: "6,x", NodeTags: " us,hk "})
	f.Plan(t, subscribe.Subscribe{Id: 3, Nodes: "9"})
	future := time.Now().Add(24 * time.Hour)
	mine := []*usersub.Subscribe{
		f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, Status: usersub.SubscribeStatusActive, ExpireTime: future}),
		f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 2, Status: usersub.SubscribeStatusExpired, ExpireTime: future}),
	}
	other := f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 3, Status: usersub.SubscribeStatusActive, ExpireTime: future})

	ctx := context.Background()
	subs := f.Store.UserSubscription()
	for _, sub := range append(append([]*usersub.Subscribe{}, mine...), other) {
		if _, err := subs.FindOneSubscribeByToken(ctx, sub.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := subs.FindOneSubscribe(ctx, sub.Id); err != nil {
			t.Fatal(err)
		}
		if _, err := subs.QueryUserSubscribe(ctx, sub.UserId); err != nil {
			t.Fatal(err)
		}
		for _, key := range sub.GetCacheKeys() {
			if !f.Cached(key) {
				t.Fatalf("setup did not cache %s", key)
			}
		}
	}

	nodeIDs, tags, err := newService(f).ClearUserCaches(ctx, []int64{7, 7, 12})
	if err != nil {
		t.Fatalf("ClearUserCaches() error = %v", err)
	}
	if !reflect.DeepEqual(nodeIDs, []int64{5, 6}) || !reflect.DeepEqual(tags, []string{"hk", "us"}) {
		t.Fatalf("scope = %v %q, want [5 6] [hk us]", nodeIDs, tags)
	}
	for _, sub := range mine {
		for _, key := range sub.GetCacheKeys() {
			if f.Cached(key) {
				t.Fatalf("cleared account's %s is still cached", key)
			}
		}
	}
	for _, key := range other.GetCacheKeys() {
		if !f.Cached(key) {
			t.Fatalf("another account's %s was dropped", key)
		}
	}

	nodeIDs, tags, err = newService(f).ClearUserCaches(ctx, []int64{12})
	if err != nil || len(nodeIDs) != 0 || len(tags) != 0 {
		t.Fatalf("an account without subscriptions reported %v %q, %v", nodeIDs, tags, err)
	}
}
