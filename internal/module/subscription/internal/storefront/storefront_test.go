package storefront

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	dto "github.com/perfect-panel/server/internal/module/subscription/contract"
	"github.com/perfect-panel/server/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/server/pkg/xerr"
)

// planNodes is a compile-checked NodeLister returning the nodes of a scope
// and counting the lookups.
type planNodes struct {
	nodes []*node.Node
	calls int
}

var _ NodeLister = (*planNodes)(nil)

func (p *planNodes) ListEnabledNodesByScope(_ context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error) {
	p.calls++
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return nil, errors.New("an empty scope lists every node")
	}
	return p.nodes, nil
}

func newStorefront(f *subtest.Fixture, nodes NodeLister, trialPlan int64) *Service {
	return NewService(Deps{
		Plans:       f.Store.Subscribe(),
		UserSubs:    f.Store.UserSubscription(),
		Nodes:       nodes,
		IsTrialPlan: func(planID int64) bool { return planID == trialPlan },
	})
}

func ownerContext(id int64) context.Context {
	return user.NewContext(context.Background(), &user.User{Id: id})
}

// The owner's node list shows nodes exactly for the subscriptions the nodes
// serve: an exhausted or expired one, even a Finished one with its counters
// cleared, lists none.
func TestQueryUserSubscribeNodeListShowsNodesOnlyForServableSubscriptions(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5"})
	now := time.Now()
	future := now.Add(24 * time.Hour)
	active := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusActive, UUID: "active-uuid"})
	sameplan := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 10, Status: usersub.SubscribeStatusActive})
	exhausted := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Traffic: 100, Upload: 100, Status: usersub.SubscribeStatusFinished})
	clearedButFinished := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Traffic: 100, Status: usersub.SubscribeStatusFinished})
	pastTerm := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: now.Add(-time.Minute), Status: usersub.SubscribeStatusActive})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusStopped})
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusActive})

	nodes := &planNodes{nodes: []*node.Node{{Id: 5, Name: "Tokyo", Protocol: "vless", Port: 443, Address: "jp.example.com", Tags: "asia", Server: &node.Server{Country: "JP", City: "Tokyo"}}}}
	resp, err := newStorefront(f, nodes, 1).QueryUserSubscribeNodeList(ownerContext(7))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]dto.UserSubscribeInfo{}
	for _, info := range resp.List {
		byID[info.Id] = info
	}
	if len(byID) != 5 {
		t.Fatalf("listed %d subscriptions, want the owner's 5 in their term", len(byID))
	}
	for _, id := range []int64{active.Id, sameplan.Id} {
		info := byID[id]
		if len(info.Nodes) != 1 || info.Nodes[0].Name != "Tokyo" || info.Nodes[0].Country != "JP" || !info.IsTryOut {
			t.Fatalf("servable subscription %d: %+v", id, info)
		}
	}
	if byID[active.Id].Nodes[0].Uuid != "active-uuid" {
		t.Fatalf("node credential = %q", byID[active.Id].Nodes[0].Uuid)
	}
	for _, id := range []int64{exhausted.Id, clearedButFinished.Id, pastTerm.Id} {
		if nodes := byID[id].Nodes; nodes != nil {
			t.Fatalf("subscription %d that no node serves lists %d nodes", id, len(nodes))
		}
	}
	if nodes.calls != 1 {
		t.Fatalf("plan nodes loaded %d times, want once per plan", nodes.calls)
	}
}

// A plan that selects no nodes has none, as in delivery: the empty scope is
// not a query for every node.
func TestQueryUserSubscribeNodeListPlanWithoutNodes(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, NodeTags: " , "})
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	nodes := &planNodes{}
	resp, err := newStorefront(f, nodes, 0).QueryUserSubscribeNodeList(ownerContext(7))
	if err != nil {
		t.Fatal(err)
	}
	if nodes.calls != 0 || len(resp.List) != 1 || resp.List[0].Id != sub.Id || resp.List[0].Nodes == nil || len(resp.List[0].Nodes) != 0 || resp.List[0].IsTryOut {
		t.Fatalf("node lookups %d, list %+v", nodes.calls, resp.List)
	}
}

func TestQueryUserSubscribeNodeListNeedsTheOwner(t *testing.T) {
	f := subtest.New(t)
	_, err := newStorefront(f, &planNodes{}, 0).QueryUserSubscribeNodeList(context.Background())
	if xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("anonymous node list = %v", err)
	}
}

func TestQuerySubscribeListShowsSellablePlans(t *testing.T) {
	f := subtest.New(t)
	sell, hide := true, false
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", Sell: &sell, Show: &sell, Discount: `[{"quantity":12,"discount":90}]`})
	f.Plan(t, subscribe.Subscribe{Id: 2, Name: "retired", Sell: &hide, Show: &sell})
	resp, err := newStorefront(f, &planNodes{}, 0).QuerySubscribeList(context.Background(), &dto.QuerySubscribeListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || len(resp.List) != 1 || resp.List[0].Name != "gold" || len(resp.List[0].Discount) != 1 || resp.List[0].Discount[0].Quantity != 12 {
		t.Fatalf("storefront plans = %+v", resp)
	}
}

func TestNowhereNodeCredentialsMatchNW3WithoutChangingOtherProtocols(t *testing.T) {
	const credential = "550e8400-e29b-41d4-a716-446655440000"
	const key = "550e8400e29b41d4a716446655440000"
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5,6"})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive, UUID: credential})
	nodes := &planNodes{nodes: []*node.Node{{Id: 5, Protocol: "nowhere", Server: &node.Server{}}, {Id: 6, Protocol: "vless", Server: &node.Server{}}}}
	resp, err := newStorefront(f, nodes, 0).QueryUserSubscribeNodeList(ownerContext(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.List) != 1 || len(resp.List[0].Nodes) != 2 {
		t.Fatalf("nodes=%+v", resp)
	}
	if resp.List[0].Nodes[0].Uuid != key || resp.List[0].Nodes[1].Uuid != credential {
		t.Fatalf("credentials=%+v", resp.List[0].Nodes)
	}
}
