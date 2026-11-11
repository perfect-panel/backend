package serverapi

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
)

// Blocks the database reads after a simultaneous cache miss. The underlying
// scope fixture supplies immutable subscriptions/accounts; these overrides
// avoid its single-reader counters and model multiple distinct servers.
type refreshScopeStore struct {
	*scopeStore
	entered chan int64
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
	builds  atomic.Int32
}

func (s *refreshScopeStore) FindOneServer(_ context.Context, id int64) (*node.Server, error) {
	return &node.Server{Id: id}, nil
}

func (s *refreshScopeStore) ListNodes(ctx context.Context, params *node.FilterNodeParams) ([]*node.Node, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.peak.Load(); active > previous; previous = s.peak.Load() {
		if s.peak.CompareAndSwap(previous, active) {
			break
		}
	}
	s.builds.Add(1)
	s.entered <- params.ServerId[0]
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []*node.Node{{Id: 11, ServerId: params.ServerId[0], Protocol: params.Protocol}}, nil
}

func (s *refreshScopeStore) SetServerCache(ctx context.Context, serverID int64, key string, value any, generation int64) error {
	current, err := s.ServerCacheGeneration(ctx, serverID)
	if err != nil || current != generation {
		return err
	}
	return s.redis.Set(ctx, key, value, time.Minute).Err()
}

func TestUserListRefreshBurstWaitsAcrossServerScopes(t *testing.T) {
	for _, mode := range []string{"cold-device", "invalidated-device", "cold-user-list"} {
		t.Run(mode, func(t *testing.T) { testUserListRefreshBurst(t, mode) })
	}
}

func testUserListRefreshBurst(t *testing.T, mode string) {
	t.Helper()
	const scopes, callersPerScope = 12, 3
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	deps, store, _ := newScopeDeps(t)
	refresh := &refreshScopeStore{scopeStore: store, entered: make(chan int64, scopes), release: make(chan struct{})}
	deps.Servers, deps.Nodes, deps.Caches = refresh, refresh, refresh
	svc := NewService(deps)
	if mode == "invalidated-device" {
		invalidateRefreshScopes(t, svc, scopes)
	}
	var once sync.Once
	unblock := func() { once.Do(func() { close(refresh.release) }) }
	var group sync.WaitGroup
	t.Cleanup(func() { unblock(); group.Wait() })
	results := make(chan error, scopes*callersPerScope)
	for id := int64(1); id <= scopes; id++ {
		for range callersPerScope {
			group.Go(func() { results <- requestRefreshedScope(ctx, svc, id, mode) })
		}
	}
	for range 4 {
		select {
		case <-refresh.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("four database builds did not start")
		}
	}
	select {
	case err := <-results:
		t.Fatalf("normal refresh burst returned before capacity became available: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	group.Wait()
	for range scopes * callersPerScope {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if refresh.peak.Load() > 4 || refresh.builds.Load() != scopes {
		t.Fatalf("peak=%d builds=%d, want at most four active and one build per scope", refresh.peak.Load(), refresh.builds.Load())
	}
	if mode == "invalidated-device" {
		for id := int64(1); id <= scopes; id++ {
			revoked := claimRequest(id, "10.0.0.2")
			revoked.SID = 99
			if _, err := svc.ClaimServerDevice(t.Context(), revoked); err == nil {
				t.Fatalf("revoked SID admitted on server %d", id)
			}
		}
	}
}

func invalidateRefreshScopes(t *testing.T, svc *Service, scopes int64) {
	t.Helper()
	for id := int64(1); id <= scopes; id++ {
		cacheIndexUsers(t, svc.deps.Redis, id, "vless", time.Minute, dto.ServerUser{Id: 99, DeviceLimit: 1})
		if _, err := svc.servedUserIndex(t.Context(), id, "vless"); err != nil {
			t.Fatal(err)
		}
	}
	// A plan-wide subscription change removes the former SID and adds SID 21
	// across every server before new claims arrive.
	for id := int64(1); id <= scopes; id++ {
		pipe := svc.deps.Redis.TxPipeline()
		pipe.Incr(t.Context(), fmt.Sprintf(node.ServerCacheGenerationKey, id))
		pipe.Del(t.Context(), serverUserListCacheKey(id, "vless"))
		if _, err := pipe.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func requestRefreshedScope(ctx context.Context, svc *Service, id int64, mode string) error {
	if mode == "cold-user-list" {
		list, _, err := svc.GetServerUserList(ctx, &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: id, Protocol: "vless"}}, RequestMeta{})
		if err != nil {
			return err
		}
		if len(list.Users) != 1 || list.Users[0].Id != 21 {
			return fmt.Errorf("unexpected list for server %d: %+v", id, list)
		}
		return nil
	}
	claim, err := svc.ClaimServerDevice(ctx, claimRequest(id, "10.0.0.1"))
	if err != nil {
		return err
	}
	if !claim.Allowed {
		return fmt.Errorf("new SID denied on server %d", id)
	}
	return nil
}
