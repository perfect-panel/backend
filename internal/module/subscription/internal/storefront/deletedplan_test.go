package storefront

import (
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/module/subscription/internal/subtest"
)

// A subscription whose plan was deleted is listed without nodes, and the
// owner's other subscriptions keep theirs: the list does not fail on it.
func TestQueryUserSubscribeNodeListSkipsADeletedPlan(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5"})
	future := time.Now().Add(24 * time.Hour)
	kept := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusActive})
	orphaned := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 99, ExpireTime: future, Status: usersub.SubscribeStatusActive})

	nodes := &planNodes{nodes: []*node.Node{{Id: 5, Name: "Tokyo", Protocol: "vless", Server: &node.Server{}}}}
	resp, err := newStorefront(f, nodes, 0).QueryUserSubscribeNodeList(ownerContext())
	if err != nil {
		t.Fatalf("QueryUserSubscribeNodeList with a deleted plan: %v", err)
	}
	if len(resp.List) != 2 {
		t.Fatalf("listed %d subscriptions, want both", len(resp.List))
	}
	for _, info := range resp.List {
		switch info.Id {
		case kept.Id:
			if len(info.Nodes) != 1 {
				t.Fatalf("the subscription of the kept plan lists %d nodes", len(info.Nodes))
			}
		case orphaned.Id:
			if len(info.Nodes) != 0 {
				t.Fatalf("the subscription of the deleted plan lists %d nodes", len(info.Nodes))
			}
		}
	}
}
