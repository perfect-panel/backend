package serverapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/redis/go-redis/v9"
)

const nw3SubscriptionUUID = "550e8400-e29b-41d4-a716-446655440000"
const nw3SubscriptionKey = "550e8400e29b41d4a716446655440000"

type nowhereScopeStore struct {
	*scopeStore
	served []subscription.ServedSubscription
}

func (s *nowhereScopeStore) ListNodes(_ context.Context, params *node.FilterNodeParams) ([]*node.Node, error) {
	return []*node.Node{{Id: 11, ServerId: s.server.Id, Protocol: params.Protocol}}, nil
}
func (s *nowhereScopeStore) ServableSubscriptionsByNodeScope(context.Context, []int64, []string) ([]subscription.ServedSubscription, error) {
	return s.served, nil
}
func newNowhereScope(t *testing.T) (Deps, *nowhereScopeStore, *redis.Client) {
	t.Helper()
	deps, base, client := newScopeDeps(t)
	base.server.Protocols = `[{"type":"nowhere","version":1,"enable":true,"port":443,"security":"tls","sni":"nw.example","cert_mode":"self","alpn":["now/1"]}]`
	store := &nowhereScopeStore{scopeStore: base, served: []subscription.ServedSubscription{{Subscription: &usersub.Subscribe{Id: 1, UserId: 100, UUID: nw3SubscriptionUUID}, Plan: &subscribe.Subscribe{Id: 7, SpeedLimit: 20, DeviceLimit: 3}}}}
	store.placeholderOwner = store.served[0].Subscription
	deps.Overrides = store
	deps.Nodes = store
	deps.Subscriptions = store
	return deps, store, client
}
func TestNowhereUserListProjectsKeyAndSkipsOldCacheETag(t *testing.T) {
	deps, store, client := newNowhereScope(t)
	svc := NewService(deps)
	old, _ := json.Marshal(dto.GetServerUserListResponse{Users: []dto.ServerUser{{Id: 1, UUID: nw3SubscriptionUUID}}})
	oldKey := fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, 4, Nowhere)
	if err := client.Set(t.Context(), oldKey, old, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	req := &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: Nowhere}}
	resp, meta, err := svc.GetServerUserList(t.Context(), req, RequestMeta{IfNoneMatch: httpx.GenerateETag(old)})
	if err != nil || len(resp.Users) != 1 {
		t.Fatalf("NW3 list=%+v %v", resp, err)
	}
	user := resp.Users[0]
	if user.Id != 1 || user.UUID != nw3SubscriptionKey || user.SpeedLimit != 20 || user.DeviceLimit != 3 {
		t.Fatalf("user=%+v", user)
	}
	if store.served[0].Subscription.UUID != nw3SubscriptionUUID {
		t.Fatal("changed persisted subscription credential")
	}
	if meta.Headers["ETag"] == httpx.GenerateETag(old) {
		t.Fatal("old UUID ETag was reused")
	}
	cached, cachedMeta, err := svc.GetServerUserList(t.Context(), req, RequestMeta{})
	if err != nil || cached.Users[0] != user || cachedMeta.Headers["ETag"] != meta.Headers["ETag"] {
		t.Fatal("NW3 cache is not stable")
	}
	index, err := svc.servedUserIndex(t.Context(), 4, Nowhere)
	if err != nil || len(index.ids) != 1 || index.limits[1] != 3 {
		t.Fatalf("real subscription ID 1 lost authorization: %+v %v", index, err)
	}
	other, _, err := svc.GetServerUserList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}}, RequestMeta{})
	if err != nil || other.Users[0].UUID != nw3SubscriptionUUID {
		t.Fatal("another protocol's UUID changed")
	}
}
func TestNowherePlaceholderIsValidButCannotAuthorizeReports(t *testing.T) {
	deps, store, _ := newNowhereScope(t)
	store.served = nil
	svc := NewService(deps)
	req := &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: Nowhere}}
	resp, _, err := svc.GetServerUserList(t.Context(), req, RequestMeta{})
	if err != nil || len(resp.Users) != 1 {
		t.Fatal(err)
	}
	key, err := node.NowhereSharedKey(resp.Users[0].UUID)
	if err != nil || key != resp.Users[0].UUID || len(key) != 32 {
		t.Fatal("placeholder is not a valid NW3 key")
	}
	index, err := svc.servedUserIndex(t.Context(), 4, Nowhere)
	if err != nil || len(index.ids) != 0 {
		t.Fatalf("placeholder granted report/device scope: %+v %v", index, err)
	}
}
func TestNowhereRejectsInvalidCredentialWithoutCachingOrLeakingIt(t *testing.T) {
	deps, store, client := newNowhereScope(t)
	store.served[0].Subscription.UUID = "invalid-private-credential"
	svc := NewService(deps)
	_, _, err := svc.GetServerUserList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: Nowhere}}, RequestMeta{})
	if err == nil || strings.Contains(err.Error(), store.served[0].Subscription.UUID) {
		t.Fatalf("invalid credential error=%v", err)
	}
	if client.Exists(t.Context(), serverUserListCacheKey(4, Nowhere)).Val() != 0 {
		t.Fatal("cached invalid credential")
	}
}
func TestStoredNowhereProtocolDistributionUsesNW3(t *testing.T) {
	deps, _, _ := newNowhereScope(t)
	response, err := NewService(deps).QueryServerProtocolConfig(t.Context(), &dto.QueryServerConfigRequest{ServerID: 4, Protocols: []string{Nowhere}})
	if err != nil || len(response.Protocols) != 1 {
		t.Fatalf("configuration=%+v %v", response, err)
	}
	p := response.Protocols[0]
	if p.Version != 3 || len(p.ALPN) != 1 || p.ALPN[0] != "nw3" || p.Obfs != "morph" {
		t.Fatalf("configuration=%+v", p)
	}
}

func (s *nowhereScopeStore) FindServerConfigOverride(context.Context, int64) (*node.ServerConfigOverride, error) {
	return nil, nil
}
