package traffic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/pars-aria-labs/asynq"
	"github.com/redis/go-redis/v9"
)

type recordingResetter struct {
	calls int
	err   error
}

var _ CalendarTrafficResetter = (*recordingResetter)(nil)

func (r *recordingResetter) ResetCalendarTraffic(context.Context) error {
	r.calls++
	return r.err
}

// The handler only runs the module's reset under the run lock and hands its
// error back to asynq, which owns the retries.
func TestResetTrafficHandlerRunsTheModuleResetUnderTheLock(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	task := asynq.NewTask("scheduler:reset:traffic", nil)

	resetter := &recordingResetter{}
	handler := NewResetTrafficHandler(resetter, client)
	if err := handler.ProcessTask(ctx, task); err != nil || resetter.calls != 1 {
		t.Fatalf("run = %v, calls %d", err, resetter.calls)
	}
	if server.Exists(resetTrafficLockKey) {
		t.Fatal("the run left its lock behind")
	}

	resetter.err = errors.New("database unavailable")
	if err := handler.ProcessTask(ctx, task); !errors.Is(err, resetter.err) {
		t.Fatalf("failed run = %v, want the reset's error for asynq to retry", err)
	}
	if server.Exists(resetTrafficLockKey) {
		t.Fatal("a failed run left its lock behind")
	}

	// Another run holds the lock: this one skips without resetting.
	if err := server.Set(resetTrafficLockKey, "other-run"); err != nil {
		t.Fatal(err)
	}
	server.SetTTL(resetTrafficLockKey, time.Minute)
	resetter.err = nil
	if err := handler.ProcessTask(ctx, task); err != nil || resetter.calls != 2 {
		t.Fatalf("overlapping run = %v, calls %d; want skipped", err, resetter.calls)
	}
	if got, _ := server.Get(resetTrafficLockKey); got != "other-run" {
		t.Fatalf("the skipped run touched the other run's lock: %q", got)
	}
}
