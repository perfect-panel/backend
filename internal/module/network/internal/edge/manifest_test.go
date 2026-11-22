package edge

import (
	"context"
	"errors"
	"testing"
	"time"

	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"gorm.io/gorm"
)

// edgeManifestUserSubs resolves every token to sub.
type edgeManifestUserSubs struct {
	sub *usersub.Subscribe
}

var _ TokenResolver = edgeManifestUserSubs{}

func (r edgeManifestUserSubs) SubscriptionByToken(context.Context, string) (*usersub.Subscribe, error) {
	if r.sub == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return r.sub, nil
}

// edgeManifestUsers reads the one account.
type edgeManifestUsers struct {
	user *userEntity.User
}

var _ AccountStateReader = edgeManifestUsers{}

func (r edgeManifestUsers) FindAccountState(context.Context, int64) (*userEntity.AccountState, error) {
	return &userEntity.AccountState{
		Id: r.user.Id, Enable: r.user.Enable, UpdatedAt: r.user.UpdatedAt, DeletedAt: r.user.DeletedAt,
	}, nil
}

// edgeManifestPlans reads the one plan.
type edgeManifestPlans struct {
	plan *subscribe.Subscribe
}

var _ PlanReader = edgeManifestPlans{}

func (r edgeManifestPlans) PlanByID(context.Context, int64) (*subscribe.Subscribe, error) {
	return r.plan, nil
}

// edgeManifestNodes serves the nodes and counts the lookups.
type edgeManifestNodes struct {
	nodes []*node.Node
	calls int
}

var _ NodeLister = (*edgeManifestNodes)(nil)

func (r *edgeManifestNodes) ListNodesByScope(_ context.Context, _ []int64, _ []string, enabled *bool, preload bool) ([]*node.Node, error) {
	r.calls++
	if enabled == nil || !*enabled || !preload {
		return nil, errors.New("the manifest lists enabled nodes with their server")
	}
	return r.nodes, nil
}

func TestProxyFromProtocol(t *testing.T) {
	item := &node.Node{Id: 7, Name: "Tokyo", Address: "jp.example.com", Port: 443, Protocol: "vless", Tags: "asia, premium", Sort: 3}
	protocol := node.Protocol{
		Type:      "vless",
		Enable:    true,
		Security:  "tls",
		SNI:       "jp.example.com",
		Transport: "ws",
		Host:      "cdn.example.com",
		Path:      "/ws",
		Flow:      "xtls-rprx-vision",
	}

	proxy, supported, reason := proxyFromProtocol(item, protocol, "00000000-0000-4000-8000-000000000001")
	if !supported || reason != "" {
		t.Fatalf("expected proxy to be supported, got supported=%v reason=%q", supported, reason)
	}
	if proxy.UUID == "" || proxy.TLS == nil || proxy.Transport == nil {
		t.Fatalf("expected credentials, tls and transport, got %#v", proxy)
	}
	if proxy.Transport.Type != "ws" || proxy.Transport.Host != "cdn.example.com" {
		t.Fatalf("unexpected transport: %#v", proxy.Transport)
	}
}

func TestProxyFromProtocolRejectsUnsupportedWorkerFeatures(t *testing.T) {
	item := &node.Node{Name: "Reality", Address: "example.com", Port: 443}
	_, supported, reason := proxyFromProtocol(item, node.Protocol{Type: "vless", Enable: true, Security: "reality"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected reality node to be rejected, got supported=%v reason=%q", supported, reason)
	}

	_, supported, reason = proxyFromProtocol(item, node.Protocol{Type: "shadowsocks", Enable: true, Cipher: "2022-blake3-aes-128-gcm"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected shadowsocks 2022 node to be rejected, got supported=%v reason=%q", supported, reason)
	}

	_, supported, reason = proxyFromProtocol(item, node.Protocol{Type: "shadowsocks", Enable: true, Cipher: "aes-128-gcm", Security: "tls"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected Shadowsocks TLS node to be rejected, got supported=%v reason=%q", supported, reason)
	}
}

// The manifest state follows the rule the node user list and delivery use,
// so "active" (the only state that lists proxies) means the nodes accept the
// user.
func TestSubscriptionState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	tests := []struct {
		name string
		sub  usersub.Subscribe
		want string
	}{
		{"active", usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: future}, "active"},
		{"no time limit", usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: usersub.NoLimitExpiry()}, "active"},
		{"legacy pending is served like the node list serves it", usersub.Subscribe{Status: usersub.SubscribeStatusPending, ExpireTime: future}, "active"},
		{"traffic used up", usersub.Subscribe{Status: usersub.SubscribeStatusActive, Traffic: 100, Upload: 40, Download: 60}, "traffic_exhausted"},
		{"finished is exhausted, not expired", usersub.Subscribe{Status: usersub.SubscribeStatusFinished, ExpireTime: future}, "traffic_exhausted"},
		{"past its term", usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: past}, "expired"},
		{"expired status", usersub.Subscribe{Status: usersub.SubscribeStatusExpired, ExpireTime: future}, "expired"},
		{"stopped", usersub.Subscribe{Status: usersub.SubscribeStatusStopped}, "suspended"},
		{"refunded", usersub.Subscribe{Status: usersub.SubscribeStatusDeducted, ExpireTime: future}, "disabled"},
		{"unknown status", usersub.Subscribe{Status: 255}, "disabled"},
	}
	for _, tt := range tests {
		sub := tt.sub
		if state := subscriptionState(&sub, now); state != tt.want {
			t.Fatalf("%s: state = %q, want %q", tt.name, state, tt.want)
		}
		if (tt.want == "active") != sub.ServableAt(now) {
			t.Fatalf("%s: manifest state disagrees with the servable rule", tt.name)
		}
	}
}

func TestManifestHidesDeletedAccount(t *testing.T) {
	enabled := true
	service := NewService(Deps{
		Subscriptions: edgeManifestUserSubs{sub: &usersub.Subscribe{UserId: 9}},
		Accounts: edgeManifestUsers{user: &userEntity.User{
			Id: 9, Enable: &enabled, DeletedAt: gorm.DeletedAt{Valid: true},
		}},
	})

	if _, err := service.Manifest(context.Background(), "deleted-user-token"); !errors.Is(err, ErrManifestNotFound) {
		t.Fatalf("Manifest error = %v, want ErrManifestNotFound", err)
	}
	service.deps.Subscriptions = edgeManifestUserSubs{}
	if _, err := service.Manifest(context.Background(), "unknown-token"); !errors.Is(err, ErrManifestNotFound) {
		t.Fatalf("Manifest error = %v, want ErrManifestNotFound", err)
	}
}

// An active subscription lists the plan's nodes the Worker supports and a
// notice for the others; one no node serves lists none and says why, without
// reading the nodes.
func TestManifestListsProxiesOnlyForServableSubscriptions(t *testing.T) {
	enabled := true
	server := &node.Server{}
	if err := server.MarshalProtocols([]node.Protocol{
		{Type: "vless", Enable: true, Security: "tls", SNI: "jp.example.com"},
		{Type: "trojan", Enable: true, Security: "reality"},
	}); err != nil {
		t.Fatal(err)
	}
	nodes := &edgeManifestNodes{nodes: []*node.Node{
		{Id: 1, Name: "Tokyo", Address: "jp.example.com", Port: 443, Protocol: "vless", Server: server},
		{Id: 2, Name: "Osaka", Address: "osaka.example.com", Port: 443, Protocol: "trojan", Server: server},
	}}
	future := time.Now().Add(24 * time.Hour)
	manifest := func(sub usersub.Subscribe) manifestResult {
		t.Helper()
		sub.UserId, sub.SubscribeId, sub.UUID = 9, 3, "00000000-0000-4000-8000-000000000009"
		service := NewService(Deps{
			Subscriptions: edgeManifestUserSubs{sub: &sub},
			Accounts:      edgeManifestUsers{user: &userEntity.User{Id: 9, Enable: &enabled}},
			Plans:         edgeManifestPlans{plan: &subscribe.Subscribe{Id: 3, Name: "gold", Nodes: "1,2"}},
			Nodes:         nodes,
			Config:        func() Snapshot { return Snapshot{} },
		})
		resp, err := service.Manifest(context.Background(), "token")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, proxy := range resp.Proxies {
			names = append(names, proxy.Name+"@"+proxy.UUID)
		}
		return manifestResult{state: resp.Subscription.State, proxies: names, notices: resp.Notices, revision: resp.Revision}
	}

	active := manifest(usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: future, Traffic: 100, Upload: 10})
	if active.state != "active" || len(active.proxies) != 1 || active.proxies[0] != "Tokyo@00000000-0000-4000-8000-000000000009" ||
		len(active.notices) != 1 || nodes.calls != 1 || active.revision == "" {
		t.Fatalf("active manifest = %+v after %d node lookups", active, nodes.calls)
	}
	exhausted := manifest(usersub.Subscribe{Status: usersub.SubscribeStatusActive, ExpireTime: future, Traffic: 100, Upload: 100})
	if exhausted.state != "traffic_exhausted" || len(exhausted.proxies) != 0 || len(exhausted.notices) != 1 ||
		exhausted.notices[0] != "Subscription traffic exhausted" || nodes.calls != 1 {
		t.Fatalf("exhausted manifest = %+v after %d node lookups", exhausted, nodes.calls)
	}
}

type manifestResult struct {
	state    string
	proxies  []string
	notices  []string
	revision string
}
