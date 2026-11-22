package publicinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/geoip/geoiptest"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

var errStatBackend = errors.New("backend unavailable")

// statUsers counts the enabled accounts; the statistics read no
// authentication methods.
type statUsers struct {
	enabled int64
	err     error
	calls   atomic.Int64
	// release, when set, holds the count until it is closed.
	release chan struct{}
}

func (u *statUsers) CountEnabledUsers(ctx context.Context) (int64, error) {
	u.calls.Add(1)
	if u.release != nil {
		select {
		case <-u.release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return u.enabled, u.err
}

func (u *statUsers) ListAuthMethods(context.Context) ([]readmodel.AuthMethod, error) {
	return nil, errors.New("the statistics read no authentication methods")
}

var (
	_ AccountStats = (*statUsers)(nil)
	_ NodeStats    = (*statNodes)(nil)
)

type statNodes struct {
	enabled                        int64
	addresses, protocols           []string
	countErr, addressErr, protoErr error
}

func (n *statNodes) CountEnabledNodes(context.Context) (int64, error) { return n.enabled, n.countErr }
func (n *statNodes) QueryServerAddresses(context.Context) ([]string, error) {
	return n.addresses, n.addressErr
}
func (n *statNodes) QueryEnabledNodeProtocols(context.Context) ([]string, error) {
	return n.protocols, n.protoErr
}

// hosts resolves the names it knows; the others fail like NXDOMAIN.
type hosts map[string]string

func (h hosts) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ip, ok := h[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
}

type statWorld struct {
	svc   *Service
	redis *miniredis.Miniredis
	users *statUsers
	nodes *statNodes
	geoip *geoip2.Reader
}

// newStatWorld locates 0.0.0.0/1 in Australia and 128.0.0.0/1 in Japan.
func newStatWorld(t *testing.T) *statWorld {
	t.Helper()
	logtest.Discard(t)
	db, err := geoip2.FromBytes(geoiptest.MMDB("GeoLite2-City",
		map[string]any{"country": map[string]any{"iso_code": "AU"}},
		map[string]any{"country": map[string]any{"iso_code": "JP"}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })

	w := &statWorld{redis: server, users: &statUsers{}, nodes: &statNodes{}, geoip: db}
	w.svc = NewService(Deps{
		Accounts: w.users,
		Nodes:    w.nodes,
		Redis:    rds,
		GeoIP:    func() *geoip2.Reader { return w.geoip },
		Resolver: hosts{"hk.example": "150.0.0.1", "au.example": "1.0.0.1"},
	})
	return w
}

func TestGetStatLocatesNodesInTheLocalDatabase(t *testing.T) {
	w := newStatWorld(t)
	w.users.enabled = 1234
	w.nodes.enabled = 7
	w.nodes.addresses = []string{
		"1.1.1.1", "2.2.2.2", "au.example", // Australia
		"hk.example", "203.0.113.7", // Japan
		"broken.example", "", // unresolvable
		"2001:db8::1", // not in the (IPv4) database
	}
	w.nodes.protocols = []string{"vless", "", "vmess", "vless", "trojan"}

	got, err := w.svc.GetStat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.GetStatResponse{User: 1200, Node: 7, Country: 2, Protocol: []string{"trojan", "vless", "vmess"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stat = %+v, want %+v", got, want)
	}

	cached, err := w.redis.Get(config.CommonStatCacheKey)
	if err != nil {
		t.Fatalf("statistics not cached: %v", err)
	}
	if data, _ := json.Marshal(want); cached != string(data) {
		t.Fatalf("cached %s, want %s", cached, data)
	}
	if ttl := w.redis.TTL(config.CommonStatCacheKey); ttl != time.Hour {
		t.Fatalf("cache ttl = %v, want 1h", ttl)
	}
}

func TestGetStatServesTheCache(t *testing.T) {
	w := newStatWorld(t)
	if err := w.redis.Set(config.CommonStatCacheKey, `{"user":500,"node":3,"country":2,"protocol":["vless"]}`); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.GetStat(context.Background())
	if err != nil || got.User != 500 || got.Country != 2 {
		t.Fatalf("stat = %+v (err %v), want the cached statistics", got, err)
	}
	if w.users.calls.Load() != 0 {
		t.Fatal("a cached read queried the store")
	}
}

func TestGetStatRoundsTheUserCount(t *testing.T) {
	for users, want := range map[int64]int64{0: 1, 7: 1, 10: 1, 11: 10, 57: 50, 100: 100, 101: 100, 1234: 1200} {
		if got := roundUserCount(users); got != want {
			t.Errorf("roundUserCount(%d) = %d, want %d", users, got, want)
		}
	}
}

// Without a GeoIP database the nodes are not located, and nothing else
// suffers.
func TestGetStatWithoutGeoIPDatabase(t *testing.T) {
	w := newStatWorld(t)
	w.geoip = nil
	w.users.enabled = 50
	w.nodes.addresses = []string{"1.1.1.1"}
	got, err := w.svc.GetStat(context.Background())
	if err != nil || got.Country != 0 || got.User != 50 {
		t.Fatalf("stat = %+v (err %v), want no countries", got, err)
	}
}

func TestGetStatReportsStoreFailures(t *testing.T) {
	for name, fail := range map[string]func(w *statWorld){
		"users":     func(w *statWorld) { w.users.err = errStatBackend },
		"nodes":     func(w *statWorld) { w.nodes.countErr = errStatBackend },
		"addresses": func(w *statWorld) { w.nodes.addressErr = errStatBackend },
		"protocols": func(w *statWorld) { w.nodes.protoErr = errStatBackend },
	} {
		t.Run(name, func(t *testing.T) {
			w := newStatWorld(t)
			fail(w)
			if _, err := w.svc.GetStat(context.Background()); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, errStatBackend) {
				t.Fatalf("err = %v, want the database failure", err)
			}
			if w.redis.Exists(config.CommonStatCacheKey) {
				t.Fatal("a failed refresh was cached")
			}
		})
	}
}

// Concurrent cache misses share a single refresh.
func TestGetStatSharesOneRefresh(t *testing.T) {
	w := newStatWorld(t)
	w.users.enabled = 300
	w.users.release = make(chan struct{})

	const callers = 8
	results := make(chan *dto.GetStatResponse, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			stat, err := w.svc.GetStat(context.Background())
			if err != nil {
				t.Error(err)
			}
			results <- stat
		})
	}
	// Let every caller join the refresh before it completes.
	waitFor(t, func() bool { return w.users.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	close(w.users.release)
	wg.Wait()
	close(results)

	for stat := range results {
		if stat == nil || stat.User != 300 {
			t.Fatalf("a caller got %+v", stat)
		}
	}
	if calls := w.users.calls.Load(); calls != 1 {
		t.Fatalf("the store was queried %d times, want once", calls)
	}
}

// A caller that gives up does not cancel the refresh for the others: its
// result still reaches the cache.
func TestGetStatCallerMayStopWaiting(t *testing.T) {
	w := newStatWorld(t)
	w.users.enabled = 300
	w.users.release = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := w.svc.GetStat(ctx)
		done <- err
	}()
	waitFor(t, func() bool { return w.users.calls.Load() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the caller's cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled caller kept waiting for the refresh")
	}

	close(w.users.release)
	waitFor(t, func() bool { return w.redis.Exists(config.CommonStatCacheKey) })
	stat, err := w.svc.GetStat(context.Background())
	if err != nil || stat.User != 300 {
		t.Fatalf("stat = %+v (err %v), want the refreshed statistics", stat, err)
	}
	if calls := w.users.calls.Load(); calls != 1 {
		t.Fatalf("the store was queried %d times, want once", calls)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// blockingResolver holds every lookup until its context ends, like a DNS
// server that drops the queries.
type blockingResolver struct{}

func (blockingResolver) LookupIPAddr(ctx context.Context, _ string) ([]net.IPAddr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// degradedDNSWorld has many hostname-addressed nodes and DNS that never
// answers; one node is addressed by IP and locates in Australia.
func degradedDNSWorld(t *testing.T) *statWorld {
	t.Helper()
	w := newStatWorld(t)
	w.svc.deps.Resolver = blockingResolver{}
	w.users.enabled = 300
	w.nodes.enabled = 50
	w.nodes.addresses = []string{"1.1.1.1"}
	for i := range 49 {
		w.nodes.addresses = append(w.nodes.addresses, fmt.Sprintf("node-%d.example", i))
	}
	return w
}

// Degraded DNS costs the statistics the countries of the hostnames that did
// not resolve within the resolution budget, not the statistics themselves:
// the call returns at the budget with the countries it could count, and the
// result is cached so that the next call does not wait again.
func TestGetStatBoundsHostnameResolution(t *testing.T) {
	w := degradedDNSWorld(t)
	w.svc.resolveTimeout = 200 * time.Millisecond

	start := time.Now()
	got, err := w.svc.GetStat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("GetStat took %v, want the resolution budget", elapsed)
	}
	if got.User != 300 || got.Node != 50 || got.Country != 1 {
		t.Fatalf("stat = %+v, want the country of the address that needed no lookup", got)
	}
	if !w.redis.Exists(config.CommonStatCacheKey) {
		t.Fatal("the statistics were not cached")
	}
	if _, err := w.svc.GetStat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := w.users.calls.Load(); calls != 1 {
		t.Fatalf("the store was queried %d times, want once: the second call reads the cache", calls)
	}
}

// A refresh that used up its whole budget still caches what it built: the
// cache write does not run on the expired refresh context.
func TestGetStatCachesARefreshThatUsedUpItsBudget(t *testing.T) {
	w := degradedDNSWorld(t)
	w.svc.refreshTimeout = 100 * time.Millisecond

	got, err := w.svc.GetStat(context.Background())
	if err != nil || got.Node != 50 {
		t.Fatalf("stat = %+v (err %v), want the statistics", got, err)
	}
	cached, err := w.redis.Get(config.CommonStatCacheKey)
	if err != nil {
		t.Fatalf("statistics not cached: %v", err)
	}
	if data, _ := json.Marshal(got); cached != string(data) {
		t.Fatalf("cached %s, want %s", cached, data)
	}
}

// panickingNodes is the network read port with a bug in its node count.
type panickingNodes struct{ *statNodes }

func (*panickingNodes) CountEnabledNodes(context.Context) (int64, error) {
	panic("node store bug")
}

// A panic inside the refresh fails the call, not the process: singleflight
// would otherwise re-raise it on a goroutine of its own, where no HTTP
// recovery catches it.
func TestGetStatSurvivesAPanicInTheRefresh(t *testing.T) {
	w := newStatWorld(t)
	w.svc.deps.Nodes = &panickingNodes{statNodes: w.nodes}

	_, err := w.svc.GetStat(context.Background())
	if err == nil || xerr.CodeOf(err) != xerr.ERROR || !strings.Contains(err.Error(), "node store bug") {
		t.Fatalf("err = %v, want the panic reported as an error", err)
	}
	if w.redis.Exists(config.CommonStatCacheKey) {
		t.Fatal("a failed refresh was cached")
	}
	// The next call after the failure backoff refreshes again.
	w.svc.statMemo.mu.Lock()
	w.svc.statMemo.failedAt = time.Now().Add(-2 * statFailureBackoff)
	w.svc.statMemo.mu.Unlock()
	w.svc.deps.Nodes = w.nodes
	w.nodes.enabled = 3
	if got, err := w.svc.GetStat(context.Background()); err != nil || got.Node != 3 {
		t.Fatalf("stat after the panic = %+v (err %v), want a fresh refresh", got, err)
	}
}
