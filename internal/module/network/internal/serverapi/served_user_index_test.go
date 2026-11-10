package serverapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/redis/go-redis/v9"
)

type listReadCounter struct{ reads atomic.Int32 }

func (h *listReadCounter) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *listReadCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.count(cmd)
		return next(ctx, cmd)
	}
}
func (h *listReadCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			h.count(cmd)
		}
		return next(ctx, cmds)
	}
}
func (h *listReadCounter) count(cmd redis.Cmder) {
	if cmd.Name() == "get" && fmt.Sprint(cmd.Args()[1]) == serverUserListCacheKey(4, "vless") {
		h.reads.Add(1)
	}
}

func cacheIndexUsers(t testing.TB, client *redis.Client, serverID int64, protocol string, ttl time.Duration, users ...dto.ServerUser) {
	t.Helper()
	payload, err := json.Marshal(dto.GetServerUserListResponse{Users: users})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(t.Context(), serverUserListCacheKey(serverID, protocol), payload, ttl).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestServedIndexSharesOneDecodeAcrossConcurrentClaimsAndReports(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 3})
	counter := &listReadCounter{}
	client.AddHook(counter)
	svc := NewService(deps)
	results := make(chan *servedUserIndex, 64)
	var group sync.WaitGroup
	for range cap(results) {
		group.Go(func() {
			index, err := svc.servedUserIndex(t.Context(), 4, "vless")
			if err != nil {
				t.Error(err)
				return
			}
			results <- index
		})
	}
	group.Wait()
	close(results)
	var first *servedUserIndex
	for index := range results {
		if first == nil {
			first = index
		}
		if first != index || index.limits[21] != 3 {
			t.Fatal("requests did not share the same index")
		}
	}
	for range 3 {
		claim, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1"))
		if err != nil || !claim.Allowed {
			t.Fatalf("claim: %+v %v", claim, err)
		}
		ids, err := svc.servedSubscriptionIDs(t.Context(), 4, "vless")
		if err != nil || len(ids) != 1 {
			t.Fatalf("report scope: %v %v", ids, err)
		}
		_, err = svc.GetServerAliveList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := counter.reads.Load(); got != 1 {
		t.Fatalf("full list reads = %d, want 1", got)
	}
}

func TestServedIndexInvalidationChangesLimitAndRevokesMembership(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	svc := NewService(deps)
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 2})
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	for _, users := range [][]dto.ServerUser{
		{{Id: 21, DeviceLimit: 1}},
		{{Id: 22, DeviceLimit: 1}},
	} {
		cacheIndexUsers(t, client, 4, "vless", time.Minute, users...)
		if err := client.Incr(t.Context(), fmt.Sprintf(node.ServerCacheGenerationKey, 4)).Err(); err != nil {
			t.Fatal(err)
		}
		resp, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.2"))
		if users[0].Id == 21 {
			if err != nil || resp.Allowed {
				t.Fatalf("changed limit not enforced: %+v %v", resp, err)
			}
		} else if err == nil {
			t.Fatal("revoked subscription was admitted")
		}
	}
	cacheIndexUsers(t, client, 5, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 7})
	cacheIndexUsers(t, client, 4, "trojan", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 9})
	for _, scope := range []struct {
		server   int64
		protocol string
		limit    int64
	}{{5, "vless", 7}, {4, "trojan", 9}} {
		index, err := svc.servedUserIndex(t.Context(), scope.server, scope.protocol)
		if err != nil || index.limits[21] != scope.limit {
			t.Fatalf("cross-scope index: %+v %v", index, err)
		}
	}
}

type placeholderRaceReader struct {
	SubscriptionReader
	onRead func()
}

func (r placeholderRaceReader) SubscriptionByID(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	r.onRead()
	return &usersub.Subscribe{Id: 1, UUID: "real-user-1"}, nil
}

func TestServedIndexDiscardsInvalidatedFill(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 1, UUID: "real-user-1"})
	deps.Subscriptions = placeholderRaceReader{SubscriptionReader: deps.Subscriptions, onRead: func() {
		cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 2})
		if err := client.Incr(t.Context(), fmt.Sprintf(node.ServerCacheGenerationKey, 4)).Err(); err != nil {
			t.Error(err)
		}
	}}
	index, err := NewService(deps).servedUserIndex(t.Context(), 4, "vless")
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := index.ids[1]; stale || index.limits[21] != 2 || index.generation != 1 {
		t.Fatalf("stale fill published: %+v", index)
	}
}

type blockingNodeLister struct {
	NodeLister
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type fencedTestCache struct{ ServerCache }

func (c fencedTestCache) SetServerCache(ctx context.Context, serverID int64, key string, value any, generation int64) error {
	current, err := c.ServerCacheGeneration(ctx, serverID)
	if err != nil || current != generation {
		return err
	}
	return c.ServerCache.SetServerCache(ctx, serverID, key, value, generation)
}

func TestServedIndexDoesNotRelabelStaleDatabaseRebuild(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	lister := &blockingNodeLister{NodeLister: deps.Nodes, entered: make(chan struct{}), release: make(chan struct{})}
	deps.Nodes = lister
	deps.Caches = fencedTestCache{deps.Caches}
	svc := NewService(deps)
	result := make(chan *servedUserIndex, 1)
	failed := make(chan error, 1)
	go func() {
		index, err := svc.servedUserIndex(t.Context(), 4, "vless")
		result <- index
		failed <- err
	}()
	<-lister.entered
	// The old database snapshot contains SID 21. Invalidate it while the fill
	// is in progress and publish the next generation with only SID 99.
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 99, DeviceLimit: 2})
	if err := client.Incr(t.Context(), fmt.Sprintf(node.ServerCacheGenerationKey, 4)).Err(); err != nil {
		t.Fatal(err)
	}
	close(lister.release)
	index, err := <-result, <-failed
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := index.ids[21]; stale || index.generation != 1 || index.limits[99] != 2 {
		t.Fatalf("stale database snapshot published: %+v", index)
	}
}

func (l *blockingNodeLister) ListNodes(ctx context.Context, params *node.FilterNodeParams) ([]*node.Node, error) {
	l.once.Do(func() { close(l.entered) })
	select {
	case <-l.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return l.NodeLister.ListNodes(ctx, params)
}

func TestUserListRebuildSharedAcrossEndpointsAndSurvivesCanceledWaiter(t *testing.T) {
	deps, store, _ := newScopeDeps(t)
	lister := &blockingNodeLister{NodeLister: deps.Nodes, entered: make(chan struct{}), release: make(chan struct{})}
	deps.Nodes = lister
	svc := NewService(deps)
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := svc.servedUserIndex(ctx, 4, "vless"); first <- err }()
	<-lister.entered
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			if i%2 == 0 {
				_, _, err := svc.GetServerUserList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}}, RequestMeta{})
				if err != nil {
					t.Error(err)
				}
			} else {
				index, err := svc.servedUserIndex(t.Context(), 4, "vless")
				if err != nil {
					t.Error(err)
					return
				}
				if _, ok := index.ids[21]; !ok {
					t.Error("missing served user")
				}
			}
		})
	}
	close(lister.release)
	group.Wait()
	if store.lookups != 1 {
		t.Fatalf("database rebuilds = %d, want 1", store.lookups)
	}
}

func TestServedIndexExpiresWithSourceList(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	cacheIndexUsers(t, client, 4, "vless", 100*time.Millisecond, dto.ServerUser{Id: 99})
	svc := NewService(deps)
	index, err := svc.servedUserIndex(t.Context(), 4, "vless")
	if err != nil {
		t.Fatal(err)
	}
	if index.expiresAt.After(time.Now().Add(100 * time.Millisecond)) {
		t.Fatal("index extended source cache lifetime")
	}
	// miniredis advances independently of wall time. Remove the source and
	// wait for the local deadline, then ensure a time-based expiry refreshes it.
	if err := client.Del(t.Context(), serverUserListCacheKey(4, "vless")).Err(); err != nil {
		t.Fatal(err)
	}
	<-time.After(time.Until(index.expiresAt) + time.Millisecond)
	next, err := svc.servedUserIndex(t.Context(), 4, "vless")
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := next.ids[99]; stale || next == index {
		t.Fatal("expired index reused")
	}
	if _, served := next.ids[21]; !served {
		t.Fatal("list was not rebuilt after expiry")
	}
}

func TestServedIndexCacheBoundsScopesAndUsers(t *testing.T) {
	var cache servedIndexCache
	for i := range maxServedIndexScopes + 10 {
		cache.put(fmt.Sprint(i), &servedUserIndex{expiresAt: time.Now().Add(time.Minute), ids: map[int64]struct{}{21: {}}})
	}
	if cache.order.Len() != maxServedIndexScopes || cache.users != maxServedIndexScopes {
		t.Fatal("scope cache is unbounded")
	}
	large := &servedUserIndex{ids: make(map[int64]struct{}, maxServedIndexUsers+1)}
	for i := range maxServedIndexUsers + 1 {
		large.ids[int64(i)] = struct{}{}
	}
	cache.put("large", large)
	if cache.order.Len() != 1 || cache.users != len(large.ids) {
		t.Fatal("oversized scope was not isolated")
	}
	cache.put("small", &servedUserIndex{ids: map[int64]struct{}{21: {}}})
	if cache.order.Len() != 1 || cache.users != 1 {
		t.Fatal("aggregate entry budget not enforced")
	}
}

type serverReaderFunc func(context.Context, int64) (*node.Server, error)

func (f serverReaderFunc) FindOneServer(ctx context.Context, id int64) (*node.Server, error) {
	return f(ctx, id)
}

func TestDeviceClaimsShedLoadBeforeRepositoryWork(t *testing.T) {
	entered := make(chan struct{}, 128)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	unavailable := errors.New("repository unavailable")
	svc := NewService(Deps{Servers: serverReaderFunc(func(ctx context.Context, _ int64) (*node.Server, error) {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, unavailable
		}
	})})
	var group sync.WaitGroup
	for range cap(svc.deviceClaims) {
		group.Go(func() {
			_, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1"))
			if !errors.Is(err, unavailable) {
				t.Errorf("pending request: %v", err)
			}
		})
	}
	for range cap(svc.deviceClaims) {
		<-entered
	}
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1")); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("overload: %v", err)
	}
	unblock()
	group.Wait()
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1")); !errors.Is(err, unavailable) {
		t.Fatalf("capacity was not released after errors: %v", err)
	}
}

func TestConcurrentIndexFillsAreBoundedAndFailuresCanRecover(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	unavailable := errors.New("repository unavailable")
	deps.Servers = serverReaderFunc(func(ctx context.Context, _ int64) (*node.Server, error) {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, unavailable
		}
	})
	svc := NewService(deps)
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 2})
	if _, err := svc.servedUserIndex(t.Context(), 4, "vless"); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := range cap(svc.indexBuilds) {
		group.Go(func() {
			_, err := svc.servedUserIndex(t.Context(), int64(10+i), "vless")
			if !errors.Is(err, unavailable) {
				t.Errorf("fill: %v", err)
			}
		})
	}
	for range cap(svc.indexBuilds) {
		<-entered
	}
	if _, err := svc.servedUserIndex(t.Context(), 20, "vless"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("unbounded cold fill: %v", err)
	}
	if _, err := svc.servedUserIndex(t.Context(), 4, "vless"); err != nil {
		t.Fatalf("warm index blocked by cold fills: %v", err)
	}
	unblock()
	group.Wait()
	cacheIndexUsers(t, client, 10, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 3})
	index, err := svc.servedUserIndex(t.Context(), 10, "vless")
	if err != nil || index.limits[21] != 3 {
		t.Fatalf("failed fill poisoned subsequent requests: %+v %v", index, err)
	}
}

// Exercise both Redis transfer and the old JSON/linear-lookup path. Miniredis
// adds a constant local transport overhead to each benchmark.
func BenchmarkServedUserLookup(b *testing.B) {
	for _, size := range []int{1000, 100000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			deps, _, client := newScopeDeps(b)
			users := make([]dto.ServerUser, size)
			for i := range users {
				users[i] = dto.ServerUser{Id: int64(i + 2), UUID: "00000000-0000-0000-0000-000000000001", DeviceLimit: 3}
			}
			cacheIndexUsers(b, client, 4, "vless", time.Hour, users...)
			svc := NewService(deps)
			if _, err := svc.servedUserIndex(b.Context(), 4, "vless"); err != nil {
				b.Fatal(err)
			}
			sid := users[len(users)-1].Id
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					index, err := svc.servedUserIndex(b.Context(), 4, "vless")
					if err != nil || index.limits[sid] != 3 {
						b.Fatal("index lookup failed", err)
					}
				}
			})
			b.Run("previous", func(b *testing.B) {
				benchmarkPreviousLookup(b, client, sid)
			})
		})
	}
}

func benchmarkPreviousLookup(b *testing.B, client *redis.Client, sid int64) {
	b.ReportAllocs()
	for b.Loop() {
		cached, err := client.Get(b.Context(), serverUserListCacheKey(4, "vless")).Result()
		if err != nil {
			b.Fatal(err)
		}
		var response dto.GetServerUserListResponse
		if err := json.Unmarshal([]byte(cached), &response); err != nil {
			b.Fatal(err)
		}
		var limit int64
		for _, user := range response.Users {
			if user.Id == sid {
				limit = user.DeviceLimit
				break
			}
		}
		if limit != 3 {
			b.Fatal("lookup failed")
		}
	}
}

var _ redis.Hook = (*listReadCounter)(nil)
