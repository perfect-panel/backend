package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// deliveryNodeRepo serves one real node, with the server protocol the
// renderer maps it through, and counts the lookups.
type deliveryNodeRepo struct {
	calls int
}

var _ NodeLister = (*deliveryNodeRepo)(nil)

func (r *deliveryNodeRepo) ListEnabledNodesByScope(context.Context, []int64, []string) ([]*node.Node, error) {
	r.calls++
	return []*node.Node{{
		Id: 5, Name: "real", Address: "node.example.com", Port: 443, Protocol: "vless",
		Server: &node.Server{Id: 1, Name: "real", Protocols: `[{"type":"vless","port":443,"enable":true}]`},
	}}, nil
}

// Refunded (Deducted) and admin-stopped subscriptions must not receive real
// node configs even while their term and traffic would allow it; the other
// statuses keep their existing behavior.
func TestGetServersWithholdsNodesFromDeductedAndStoppedSubscriptions(t *testing.T) {
	plan := &subscribe.Subscribe{Id: 1, Nodes: "5"}
	future := time.Now().Add(24 * time.Hour)
	tests := []struct {
		name   string
		sub    usersub.Subscribe
		notice string
	}{
		{name: "deducted", sub: usersub.Subscribe{Status: usersub.SubscribeStatusDeducted, ExpireTime: future}, notice: "Subscribe Unavailable"},
		{name: "stopped", sub: usersub.Subscribe{Status: usersub.SubscribeStatusStopped, ExpireTime: future}, notice: "Subscribe Unavailable"},
		{name: "stopped no limit", sub: usersub.Subscribe{Status: usersub.SubscribeStatusStopped, ExpireTime: time.UnixMilli(0)}, notice: "Subscribe Unavailable"},
		{name: "expired", sub: usersub.Subscribe{Status: usersub.SubscribeStatusExpired, ExpireTime: time.Now().Add(-time.Hour)}, notice: "Subscribe Expired"},
		{name: "exhausted", sub: usersub.Subscribe{Status: usersub.SubscribeStatusFinished, ExpireTime: future, Traffic: 10, Upload: 10}, notice: "Traffic Exhausted"},
		{name: "active", sub: usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: future}},
		{name: "active no limit", sub: usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: time.UnixMilli(0)}},
		{name: "pending", sub: usersub.Subscribe{Status: usersub.SubscribeStatusPending, ExpireTime: future}},
		// Delivery agrees with the node user list, which serves neither: a
		// Finished row whose counters were cleared without reactivating it,
		// and an Expired row whose term was moved without its status.
		{name: "finished with traffic left", sub: usersub.Subscribe{Status: usersub.SubscribeStatusFinished, ExpireTime: future, Traffic: 10}, notice: "Traffic Exhausted"},
		{name: "expired status with a future term", sub: usersub.Subscribe{Status: usersub.SubscribeStatusExpired, ExpireTime: future}, notice: "Subscribe Expired"},
		{name: "active past expiry", sub: usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: time.Now().Add(-time.Minute)}, notice: "Subscribe Expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := &deliveryNodeRepo{}
			svc := NewService(Deps{Nodes: nodes})
			sub := tt.sub
			servers, err := svc.getServers(context.Background(), "", &sub, plan)
			if err != nil {
				t.Fatal(err)
			}
			if tt.notice == "" {
				if nodes.calls != 1 || len(servers) != 1 || servers[0].Name != "real" {
					t.Fatalf("usable subscription lost its nodes: calls=%d servers=%+v", nodes.calls, servers)
				}
				return
			}
			if nodes.calls != 0 {
				t.Fatalf("real nodes were loaded for status %d", sub.Status)
			}
			if len(servers) == 0 {
				t.Fatal("no notice placeholder returned")
			}
			for _, server := range servers {
				if server.Address != "127.0.0.1" || server.Server == nil || !strings.Contains(server.Server.Name, tt.notice) {
					t.Fatalf("placeholder = %+v, want a %q notice on 127.0.0.1", server, tt.notice)
				}
			}
		})
	}
}
