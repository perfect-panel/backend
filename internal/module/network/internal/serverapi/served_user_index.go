package serverapi

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

const (
	userListLoadTimeout  = 10 * time.Second
	maxServedIndexScopes = 256
	maxServedIndexUsers  = 1_000_000
)

// ErrServerBusy allows the transport to shed load without queuing more work.
var ErrServerBusy = errors.New("server admission capacity exceeded")

// servedUserIndex contains only immutable authorization data, not UUIDs or
// serialized lists. A warm lookup reads one generation token and one map entry.
type servedUserIndex struct {
	generation int64
	expiresAt  time.Time
	limits     map[int64]int64
	ids        map[int64]struct{}
}

type servedIndexEntry struct {
	key   string
	index *servedUserIndex
}

// The LRU bounds idle scopes and aggregate subscription entries. A scope larger
// than the budget is kept alone, so it can still share one index across claims.
type servedIndexCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
	users   int
}

func (c *servedIndexCache) get(key string, generation int64, now time.Time) *servedUserIndex {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil {
		return nil
	}
	index := e.Value.(servedIndexEntry).index
	if index.generation != generation || !now.Before(index.expiresAt) {
		c.remove(e)
		return nil
	}
	c.order.MoveToFront(e)
	return index
}

func (c *servedIndexCache) put(key string, index *servedUserIndex) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*list.Element)
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	for c.order.Len() > 0 && (c.order.Len() >= maxServedIndexScopes || c.users+len(index.ids) > maxServedIndexUsers) {
		c.remove(c.order.Back())
	}
	c.entries[key] = c.order.PushFront(servedIndexEntry{key: key, index: index})
	c.users += len(index.ids)
}

func (c *servedIndexCache) remove(e *list.Element) {
	entry := e.Value.(servedIndexEntry)
	delete(c.entries, entry.key)
	c.users -= len(entry.index.ids)
	c.order.Remove(e)
}

func (s *Service) servedUserIndex(ctx context.Context, serverID int64, protocol string) (*servedUserIndex, error) {
	key := serverUserListCacheKey(serverID, protocol)
	generation, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if index := s.userIndexes.get(key, generation, time.Now()); index != nil {
		return index, nil
	}
	result := s.indexLoads.DoChan(key, func() (any, error) {
		return s.loadServedUserIndex(ctx, serverID, protocol)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		index := result.Val.(*servedUserIndex)
		// A waiter can join a fill just after its final generation check.
		// Recheck on delivery so it cannot consume a now-revoked snapshot.
		current, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
		if err != nil {
			return nil, err
		}
		if current != index.generation || !time.Now().Before(index.expiresAt) {
			return nil, errors.New("server user list changed during admission")
		}
		return index, nil
	}
}

func (s *Service) loadServedUserIndex(ctx context.Context, serverID int64, protocol string) (*servedUserIndex, error) {
	key := serverUserListCacheKey(serverID, protocol)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), userListLoadTimeout)
	defer cancel()
	select {
	case s.indexBuilds <- struct{}{}:
		defer func() { <-s.indexBuilds }()
	default:
		return nil, ErrServerBusy
	}
	for range 3 {
		generation, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
		if err != nil {
			return nil, err
		}
		if index := s.userIndexes.get(key, generation, time.Now()); index != nil {
			return index, nil
		}
		data, err := s.readCachedUserList(ctx, serverID, protocol, generation)
		if err != nil {
			return nil, err
		}
		if data == nil {
			data, err = s.rebuildUserList(ctx, serverID, protocol)
			if err != nil {
				return nil, err
			}
		}
		users, err := s.withoutPlaceholder(ctx, data.resp.Users)
		if err != nil {
			return nil, err
		}
		index := &servedUserIndex{
			generation: data.generation, expiresAt: data.expiresAt,
			limits: make(map[int64]int64, len(users)), ids: make(map[int64]struct{}, len(users)),
		}
		for _, user := range users {
			index.limits[user.Id] = user.DeviceLimit
			index.ids[user.Id] = struct{}{}
		}
		current, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
		if err != nil {
			return nil, err
		}
		// A list built across an invalidation must never be installed under
		// the newer generation, even if Redis rejected its stale cache fill.
		if current != index.generation || !time.Now().Before(index.expiresAt) {
			continue
		}
		s.userIndexes.put(key, index)
		return index, nil
	}
	return nil, errors.New("server user list changed during admission")
}
