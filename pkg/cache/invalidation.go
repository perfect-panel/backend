package cache

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// InvalidationQueue deduplicates cache keys that must be deleted after a
// database transaction commits. It is safe for use by repositories that share
// one Store transaction.
type InvalidationQueue struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func NewInvalidationQueue() *InvalidationQueue {
	return &InvalidationQueue{keys: make(map[string]struct{})}
}

func (q *InvalidationQueue) Add(keys ...string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, key := range keys {
		if key != "" {
			q.keys[key] = struct{}{}
		}
	}
}

func (q *InvalidationQueue) Flush(ctx context.Context, client *redis.Client) error {
	if q == nil || client == nil {
		return nil
	}
	q.mu.Lock()
	keys := make([]string, 0, len(q.keys))
	for key := range q.keys {
		keys = append(keys, key)
	}
	q.mu.Unlock()
	if len(keys) == 0 {
		return nil
	}
	if err := invalidateCacheKeys(ctx, client, keys...); err != nil {
		return err
	}
	q.mu.Lock()
	for _, key := range keys {
		delete(q.keys, key)
	}
	q.mu.Unlock()
	return nil
}

// Keys returns a snapshot of pending invalidations. It is intended for handing
// failed post-commit work to the shared retry worker.
func (q *InvalidationQueue) Keys() []string {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	keys := make([]string, 0, len(q.keys))
	for key := range q.keys {
		keys = append(keys, key)
	}
	return keys
}

// InvalidationRetrier coalesces failed post-commit invalidations into one
// bounded worker. It starts only while work is pending, so Redis outages do
// not create one goroutine per committed transaction.
type InvalidationRetrier struct {
	client *redis.Client

	mu       sync.Mutex
	keys     map[string]uint64
	sequence uint64
	running  bool
}

func NewInvalidationRetrier(client *redis.Client) *InvalidationRetrier {
	return &InvalidationRetrier{
		client: client,
		keys:   make(map[string]uint64),
	}
}

func (r *InvalidationRetrier) Enqueue(keys ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	for _, key := range keys {
		if key != "" {
			r.sequence++
			r.keys[key] = r.sequence
		}
	}
	if len(r.keys) == 0 || r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()
	go r.run()
}

func (r *InvalidationRetrier) run() {
	delay := time.Second
	for {
		pending := r.snapshot()
		if len(pending) == 0 {
			r.mu.Lock()
			if len(r.keys) == 0 {
				r.running = false
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		keys := make([]string, 0, len(pending))
		for key := range pending {
			keys = append(keys, key)
		}
		err := invalidateCacheKeys(ctx, r.client, keys...)
		cancel()
		if err == nil {
			r.remove(pending)
			delay = time.Second
			continue
		}
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func (r *InvalidationRetrier) snapshot() map[string]uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make(map[string]uint64, len(r.keys))
	for key, sequence := range r.keys {
		keys[key] = sequence
	}
	return keys
}

func (r *InvalidationRetrier) remove(keys map[string]uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, sequence := range keys {
		if r.keys[key] == sequence {
			delete(r.keys, key)
		}
	}
}

func invalidateCacheKeys(ctx context.Context, client *redis.Client, keys ...string) error {
	if client == nil || len(keys) == 0 {
		return nil
	}
	uniqueKeys := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key != "" {
			uniqueKeys[key] = struct{}{}
		}
	}
	if len(uniqueKeys) == 0 {
		return nil
	}
	_, err := client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		for key := range uniqueKeys {
			pipe.Incr(ctx, cacheVersionKey(key))
			pipe.Expire(ctx, cacheVersionKey(key), defaultExpiry)
			pipe.Del(ctx, key)
		}
		return nil
	})
	return err
}
