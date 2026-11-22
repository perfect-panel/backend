package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/pars-aria-labs/asynq"
	"github.com/redis/go-redis/v9"
)

type recordingSweeper struct {
	calls int
	err   error
}

var _ LifecycleSweeper = (*recordingSweeper)(nil)

func (s *recordingSweeper) CheckSubscriptions(context.Context) error {
	s.calls++
	return s.err
}

// The handler runs the sweep under the run lock, frees the lock whether the
// sweep succeeded or failed (asynq owns the retries), and skips a tick while
// a slow run still holds the lock, so no two sweeps finish and notify the
// same subscriptions.
func TestCheckSubscriptionHandlerRunsTheSweepOnceUnderTheLock(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	sweeper := &recordingSweeper{}
	handler := NewCheckSubscriptionHandler(sweeper, client)
	task := asynq.NewTask("scheduler:check:subscription", nil)

	for _, failure := range []error{nil, errors.New("database unavailable")} {
		sweeper.err = failure
		if err := handler.ProcessTask(ctx, task); !errors.Is(err, failure) {
			t.Fatalf("run = %v, want the sweep's %v", err, failure)
		}
		if server.Exists(checkSubscriptionLockKey) {
			t.Fatalf("a run (failure %v) left its lock behind", failure)
		}
	}
	if sweeper.calls != 2 {
		t.Fatalf("sweeps = %d, want one per run", sweeper.calls)
	}

	if err := server.Set(checkSubscriptionLockKey, "slow-run"); err != nil {
		t.Fatal(err)
	}
	server.SetTTL(checkSubscriptionLockKey, time.Minute)
	sweeper.err = nil
	if err := handler.ProcessTask(ctx, task); err != nil || sweeper.calls != 2 {
		t.Fatalf("overlapping tick = %v, sweeps %d; want skipped", err, sweeper.calls)
	}
	if got, _ := server.Get(checkSubscriptionLockKey); got != "slow-run" {
		t.Fatalf("the skipped tick touched the slow run's lock: %q", got)
	}
}
