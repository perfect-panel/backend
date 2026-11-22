package account

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

func newGuardClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	logtest.Discard(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

// Password checks against one account run out after MaxPasswordAttempts; the
// lockout ends with its window, and a correct password clears it.
func TestPasswordAttemptsRunOutForTheWindow(t *testing.T) {
	server, client := newGuardClient(t)
	ctx := context.Background()

	for i := 0; i < MaxPasswordAttempts; i++ {
		if err := ReservePasswordAttempt(ctx, client, 7); err != nil {
			t.Fatalf("attempt %d refused early: %v", i+1, err)
		}
	}
	err := ReservePasswordAttempt(ctx, client, 7)
	if code := xerr.CodeOf(err); err == nil || code != xerr.TooManyRequests {
		t.Fatalf("after %d attempts: error = %v, want TooManyRequests", MaxPasswordAttempts, err)
	}
	if ttl := server.TTL(PasswordAttemptKey(7)); ttl <= 0 || ttl > PasswordAttemptWindow {
		t.Fatalf("lockout TTL = %v, want within the %v window", ttl, PasswordAttemptWindow)
	}
	if err := ReservePasswordAttempt(ctx, client, 8); err != nil {
		t.Fatalf("another account was locked: %v", err)
	}

	server.FastForward(PasswordAttemptWindow)
	if err := ReservePasswordAttempt(ctx, client, 7); err != nil {
		t.Fatalf("lockout outlasted its window: %v", err)
	}

	ClearPasswordAttempts(ctx, client, 7)
	if server.Exists(PasswordAttemptKey(7)) {
		t.Fatal("a correct password did not clear the attempts")
	}
}

// The limit holds against a burst: attempts reserved concurrently never
// exceed it, however many run at once.
func TestConcurrentPasswordAttemptsNeverExceedTheLimit(t *testing.T) {
	_, client := newGuardClient(t)
	var granted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6*MaxPasswordAttempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ReservePasswordAttempt(context.Background(), client, 7); err == nil {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := granted.Load(); got != MaxPasswordAttempts {
		t.Fatalf("attempts granted = %d, want exactly %d", got, MaxPasswordAttempts)
	}
}

// Without a store the guard stays out of the way.
func TestPasswordGuardWithoutAStore(t *testing.T) {
	var client *redis.Client
	if err := ReservePasswordAttempt(context.Background(), client, 7); err != nil {
		t.Fatalf("ReservePasswordAttempt() = %v", err)
	}
	ClearPasswordAttempts(context.Background(), client, 7)
}
