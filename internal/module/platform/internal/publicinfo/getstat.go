package publicinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

const (
	statCacheTTL = time.Hour
	// statRefreshTimeout bounds one refresh of the statistics, which runs
	// on behalf of every caller waiting for it rather than of one request.
	statRefreshTimeout = 30 * time.Second
	// statResolveTimeout bounds the resolution of all node hostnames
	// together, well inside statRefreshTimeout: with DNS degraded, the
	// refresh gives up on the hostnames still unresolved at the deadline and
	// counts the countries of the addresses it has, so that the statistics
	// still arrive and get cached instead of every call waiting again.
	statResolveTimeout = 10 * time.Second
	// statLookupTimeout bounds the DNS lookup of one node hostname.
	statLookupTimeout = 5 * time.Second
	statLookupWorkers = 8
	// statCacheWriteTimeout bounds caching the refreshed statistics, which
	// runs on a context of its own: a refresh that used up its budget
	// building the statistics still caches them.
	statCacheWriteTimeout = 2 * time.Second
	// statFailureBackoff is how long a failed refresh is answered from
	// memory before the store is asked again: the anonymous statistics
	// endpoint must not turn a store outage into a query per call.
	statFailureBackoff = 30 * time.Second
)

// GetStat returns the public site statistics: the enabled users (rounded
// down), the enabled nodes, the number of countries the nodes are in and the
// protocols they offer. The statistics are cached for an hour in Redis and
// remembered in the process: while Redis is unreachable the process serves
// its own copy for as long as the cache would have, and a refresh that
// failed is not tried again for statFailureBackoff. Concurrent cache misses
// share one refresh, and a caller may stop waiting for it without cancelling
// it for the others.
func (s *Service) GetStat(ctx context.Context) (*dto.GetStatResponse, error) {
	if cached := s.cachedStat(ctx); cached != nil {
		return cached, nil
	}
	if err := s.statMemo.recentFailure(timeutil.Now()); err != nil {
		return nil, err
	}
	refresh := s.statRefresh.DoChan(config.CommonStatCacheKey, func() (stat any, err error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.refreshTimeout)
		defer cancel()
		// With callers waiting on its channel, singleflight re-raises a panic
		// of this function on a goroutine of its own, out of reach of the
		// HTTP recovery, and the process dies; the refresh fails instead.
		defer func() {
			if r := recover(); r != nil {
				logger.WithContext(refreshCtx).Errorw("[GetStat] refresh panicked",
					logger.Field("panic", fmt.Sprint(r)), logger.Field("stack", string(debug.Stack())))
				stat, err = nil, xerr.Errorf(xerr.ERROR, "refresh the site statistics: %v", r)
			}
			if err != nil {
				s.statMemo.failed(timeutil.Now(), err)
			}
		}()
		// A refresh that finished while this one was being scheduled has
		// already done the work.
		if cached := s.cachedStat(refreshCtx); cached != nil {
			return cached, nil
		}
		refreshed, err := s.refreshStat(refreshCtx)
		if err != nil {
			return nil, err
		}
		s.statMemo.remember(refreshed, timeutil.Now())
		return refreshed, nil
	})
	select {
	case result := <-refresh:
		if result.Err != nil {
			return nil, result.Err
		}
		// Every waiting caller gets its own copy of the shared result.
		stat := *result.Val.(*dto.GetStatResponse)
		stat.Protocol = slices.Clone(stat.Protocol)
		return &stat, nil
	case <-ctx.Done():
		return nil, xerr.Wrapf(ctx.Err(), xerr.ERROR, "wait for the site statistics: %v", ctx.Err())
	}
}

// cachedStat reads the statistics from Redis, or, when Redis cannot be
// read, from the process's own copy while that is no older than the cache
// would be.
func (s *Service) cachedStat(ctx context.Context) *dto.GetStatResponse {
	data, err := s.deps.Redis.Get(ctx, config.CommonStatCacheKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || ctx.Err() != nil {
			return nil
		}
		logger.WithContext(ctx).Errorw("[GetStat] read the cached statistics", logger.Field("error", err.Error()))
		return s.statMemo.fresh(timeutil.Now(), statCacheTTL)
	}
	var cached dto.GetStatResponse
	if json.Unmarshal([]byte(data), &cached) != nil {
		return nil
	}
	s.statMemo.remember(&cached, timeutil.Now())
	return &cached
}

// remember keeps a copy of stat, built or read at now.
func (m *statMemo) remember(stat *dto.GetStatResponse, now time.Time) {
	copied := *stat
	copied.Protocol = slices.Clone(stat.Protocol)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stat, m.at, m.err = &copied, now, nil
}

// fresh returns a copy of the remembered statistics when they are younger
// than maxAge, else nil.
func (m *statMemo) fresh(now time.Time, maxAge time.Duration) *dto.GetStatResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stat == nil || now.Sub(m.at) > maxAge {
		return nil
	}
	copied := *m.stat
	copied.Protocol = slices.Clone(m.stat.Protocol)
	return &copied
}

// failed records a refresh that failed at now with err.
func (m *statMemo) failed(now time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedAt, m.err = now, err
}

// recentFailure returns the error of a refresh that failed within the
// backoff, which is answered instead of refreshing again; nil otherwise.
func (m *statMemo) recentFailure(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err == nil || now.Sub(m.failedAt) > statFailureBackoff {
		return nil
	}
	return m.err
}

func (s *Service) refreshStat(ctx context.Context) (*dto.GetStatResponse, error) {
	nodes := s.deps.Nodes
	users, err := s.deps.Accounts.CountEnabledUsers(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "count enabled users: %v", err)
	}
	nodeCount, err := nodes.CountEnabledNodes(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "count enabled nodes: %v", err)
	}
	addresses, err := nodes.QueryServerAddresses(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list node addresses: %v", err)
	}
	protocols, err := nodes.QueryEnabledNodeProtocols(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list node protocols: %v", err)
	}

	stat := &dto.GetStatResponse{
		User:     roundUserCount(users),
		Node:     nodeCount,
		Country:  int64(s.countCountries(ctx, addresses)),
		Protocol: distinctProtocols(protocols),
	}
	data, err := json.Marshal(stat)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "encode the site statistics: %v", err)
	}
	// The refresh may have used up its budget building the statistics; not
	// caching them would make every following call build them again.
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statCacheWriteTimeout)
	defer cancel()
	if err := s.deps.Redis.Set(cacheCtx, config.CommonStatCacheKey, string(data), statCacheTTL).Err(); err != nil {
		logger.WithContext(ctx).Errorw("[GetStat] cache the statistics", logger.Field("error", err.Error()))
	}
	return stat, nil
}

// roundUserCount publishes the user count rounded down to a multiple of 100
// above 100 users and of 10 above 10; smaller sites show 1.
func roundUserCount(users int64) int64 {
	switch {
	case users > 100:
		return users - users%100
	case users > 10:
		return users - users%10
	default:
		return 1
	}
}

func distinctProtocols(protocols []string) []string {
	var distinct []string
	for _, protocol := range protocols {
		if protocol != "" && !slices.Contains(distinct, protocol) {
			distinct = append(distinct, protocol)
		}
	}
	slices.Sort(distinct)
	return distinct
}

// countCountries counts the countries the local GeoIP database places the
// node addresses in. An address that does not resolve within the resolution
// budget, or that the database has no country for, is left out: the count
// is a lower bound.
func (s *Service) countCountries(ctx context.Context, addresses []string) int {
	if len(addresses) == 0 {
		return 0
	}
	var db *geoip2.Reader
	if s.deps.GeoIP != nil {
		db = s.deps.GeoIP()
	}
	if db == nil {
		logger.WithContext(ctx).Infow("[GetStat] no GeoIP database: the node countries are not counted")
		return 0
	}
	countries := map[string]struct{}{}
	failed := 0
	for _, ip := range s.resolve(ctx, addresses) {
		record, err := db.Country(ip)
		if err != nil {
			failed++
			continue
		}
		if code := record.Country.IsoCode; code != "" {
			countries[code] = struct{}{}
		}
	}
	if failed > 0 {
		logger.WithContext(ctx).Errorw("[GetStat] locate node addresses",
			logger.Field("failed", failed), logger.Field("addresses", len(addresses)))
	}
	return len(countries)
}

// resolve returns the IPs of the node addresses, looking hostnames up with
// bounded concurrency and within the resolution budget altogether; the
// addresses that have not resolved by then are left out.
func (s *Service) resolve(ctx context.Context, addresses []string) []net.IP {
	resolver := s.deps.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, s.resolveTimeout)
	defer cancel()
	ips := make([]net.IP, len(addresses))
	slots := make(chan struct{}, statLookupWorkers)
	var (
		wg     sync.WaitGroup
		failed atomic.Int64
	)
	for i, address := range addresses {
		if ip := net.ParseIP(address); ip != nil {
			ips[i] = ip
			continue
		}
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				failed.Add(1)
				return
			}
			lookupCtx, cancel := context.WithTimeout(ctx, statLookupTimeout)
			defer cancel()
			resolved, err := resolver.LookupIPAddr(lookupCtx, address)
			if err != nil || len(resolved) == 0 {
				failed.Add(1)
				return
			}
			ips[i] = resolved[0].IP
		})
	}
	wg.Wait()
	if n := failed.Load(); n > 0 {
		logger.WithContext(ctx).Errorw("[GetStat] resolve node hostnames",
			logger.Field("failed", n), logger.Field("budget_exhausted", ctx.Err() != nil))
	}
	return slices.DeleteFunc(ips, func(ip net.IP) bool { return ip == nil })
}
