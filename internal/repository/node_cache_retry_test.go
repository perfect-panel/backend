package repository

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestServerCacheInvalidationRetrierRetriesAndStops(t *testing.T) {
	var attempts atomic.Int32
	done := make(chan struct{})
	retrier := newServerCacheInvalidationRetrierWithFunc(func(context.Context, int64) error {
		if attempts.Add(1) == 1 {
			return errors.New("redis unavailable")
		}
		close(done)
		return nil
	}, time.Millisecond)

	retrier.Enqueue(42, 42)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache invalidation was not retried")
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		retrier.mu.Lock()
		idle := !retrier.running && len(retrier.serverIDs) == 0
		retrier.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("retrier did not stop after successful invalidation")
}

func TestClearServerCacheQueuesFailureWithoutFailingWrite(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
	queued := make(chan int64, 1)
	retrier := newServerCacheInvalidationRetrierWithFunc(func(_ context.Context, serverID int64) error {
		queued <- serverID
		return nil
	}, time.Millisecond)
	repo := newNodeRepo(nil, client, retrier)

	if err := repo.ClearServerCache(context.Background(), 42); err != nil {
		t.Fatalf("cache recovery must not fail the completed database write: %v", err)
	}
	select {
	case serverID := <-queued:
		if serverID != 42 {
			t.Fatalf("queued server id = %d, want 42", serverID)
		}
	case <-time.After(time.Second):
		t.Fatal("failed server cache invalidation was not queued")
	}
}

func TestServerCacheInvalidationRetrierKeepsConcurrentInvalidation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var attempts atomic.Int32
	retrier := newServerCacheInvalidationRetrierWithFunc(func(context.Context, int64) error {
		if attempts.Add(1) == 1 {
			close(started)
			<-release
			return nil
		}
		close(done)
		return nil
	}, time.Millisecond)

	retrier.Enqueue(42)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first invalidation did not start")
	}
	retrier.Enqueue(42)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent invalidation was dropped")
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}
