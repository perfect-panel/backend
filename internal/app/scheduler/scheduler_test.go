package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/robfig/cron/v3"
)

func mustSchedule(t *testing.T, spec string) cron.Schedule {
	t.Helper()
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func TestTickSlotCollapsesReplicaTicks(t *testing.T) {
	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	every := mustSchedule(t, "@every 60s")
	a, length := tickSlot(every, base.Add(10*time.Second))
	b, _ := tickSlot(every, base.Add(40*time.Second))
	next, _ := tickSlot(every, base.Add(70*time.Second))
	if !a.Equal(b) || !a.Equal(base) || length != time.Minute {
		t.Fatalf("replica ticks in one interval got slots %v/%v (length %v), want %v", a, b, length, base)
	}
	if !next.Equal(base.Add(time.Minute)) {
		t.Fatalf("next interval slot = %v, want %v", next, base.Add(time.Minute))
	}

	daily := mustSchedule(t, "0 0 * * *")
	first, _ := tickSlot(daily, base.Add(3*time.Millisecond))
	late, _ := tickSlot(daily, base.Add(900*time.Millisecond))
	if !first.Equal(late) {
		t.Fatalf("cron ticks of one activation got slots %v/%v", first, late)
	}
}

func newTestReplica(t *testing.T, redis *miniredis.Miniredis, now time.Time) *Service {
	t.Helper()
	replica := newService(asynq.RedisClientOpt{Addr: redis.Addr()}, time.UTC)
	replica.now = func() time.Time { return now }
	t.Cleanup(func() { _ = replica.client.Close() })
	return replica
}

// Every replica ticks for the same slot; only one task may be enqueued, and
// a slot's unfinished task must not hold back the next slot.
func TestEnqueueCollapsesReplicaTicksPerSlot(t *testing.T) {
	redis := miniredis.RunT(t)
	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	task := periodicTask{spec: "@every 60s", taskType: "scheduler:test:collapse", name: "test", opts: []asynq.Option{asynq.MaxRetry(3)}}
	schedule := mustSchedule(t, task.spec)

	newTestReplica(t, redis, base.Add(10*time.Second)).enqueue(task, schedule)
	newTestReplica(t, redis, base.Add(40*time.Second)).enqueue(task, schedule)

	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr()})
	t.Cleanup(func() { _ = inspector.Close() })
	pending, err := inspector.ListPendingTasks("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending tasks = %d, want 1 per slot", len(pending))
	}
	info := pending[0]
	if info.ID != fmt.Sprintf("%s:%d", task.taskType, base.Unix()) || info.MaxRetry != 3 || info.Retention != time.Minute+slotRetentionMargin {
		t.Fatalf("slot task = id %q, max retry %d, retention %v", info.ID, info.MaxRetry, info.Retention)
	}

	// The first slot's task is still pending (a long run looks the same);
	// the next slot is enqueued regardless.
	newTestReplica(t, redis, base.Add(100*time.Second)).enqueue(task, schedule)
	if pending, err = inspector.ListPendingTasks("default"); err != nil || len(pending) != 2 {
		t.Fatalf("pending tasks after the next slot = %d, %v; want 2", len(pending), err)
	}
}

// The case asynq.Unique gets wrong: the slot's task already succeeded when a
// lagging replica ticks for the same slot. The retained completed task still
// owns the ID, so the slot runs once.
func TestEnqueueCollapsesReplicaTickAfterSlotCompleted(t *testing.T) {
	redis := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: redis.Addr()}
	base := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	task := periodicTask{spec: "0 0 * * *", taskType: "scheduler:test:completed", name: "test", opts: []asynq.Option{asynq.MaxRetry(3)}}
	schedule := mustSchedule(t, task.spec)

	var runs atomic.Int32
	worker := asynq.NewServer(opt, asynq.Config{Concurrency: 1, LogLevel: asynq.FatalLevel})
	mux := asynq.NewServeMux()
	mux.HandleFunc(task.taskType, func(context.Context, *asynq.Task) error {
		runs.Add(1)
		return nil
	})
	if err := worker.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Shutdown)

	newTestReplica(t, redis, base.Add(2*time.Millisecond)).enqueue(task, schedule)
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = inspector.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		completed, err := inspector.ListCompletedTasks("default")
		if err == nil && len(completed) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot task never completed: runs=%d err=%v", runs.Load(), err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The lagging replica's enqueue collides with the retained task ID, so
	// nothing is queued that the worker could run a second time.
	if newTestReplica(t, redis, base.Add(1500*time.Millisecond)).enqueue(task, schedule) {
		t.Fatal("lagging replica enqueued the completed slot again")
	}
	if pending, err := inspector.ListPendingTasks("default"); err != nil || len(pending) != 0 || runs.Load() != 1 {
		t.Fatalf("lagging replica re-ran the slot: runs=%d pending=%d err=%v", runs.Load(), len(pending), err)
	}
}

// Tasks that fail are retried by asynq with the budget their schedule sets:
// the calendar reset retries (each subscription resets once per day however
// often it runs), the minute-by-minute sweep leaves the retry to its next
// tick.
func TestPeriodicTaskRetryBudgets(t *testing.T) {
	want := map[string]int{
		taskqueue.SchedulerResetTraffic:      3,
		taskqueue.SchedulerCheckSubscription: 0,
	}
	for _, task := range periodicTasks {
		budget, ok := want[task.taskType]
		if !ok {
			continue
		}
		delete(want, task.taskType)
		redis := miniredis.RunT(t)
		replica := newTestReplica(t, redis, time.Date(2026, 9, 27, 0, 30, 0, 0, time.UTC))
		if !replica.enqueue(task, mustSchedule(t, task.spec)) {
			t.Fatalf("%s was not enqueued", task.name)
		}
		inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr()})
		pending, err := inspector.ListPendingTasks("default")
		_ = inspector.Close()
		if err != nil || len(pending) != 1 || pending[0].MaxRetry != budget {
			t.Fatalf("%s: pending %d, err %v, want max retry %d", task.name, len(pending), err, budget)
		}
	}
	if len(want) != 0 {
		t.Fatalf("periodic tasks missing: %v", want)
	}
}

func TestStopEndsStartAndIsIdempotent(t *testing.T) {
	redis := miniredis.RunT(t)
	replica := newService(asynq.RedisClientOpt{Addr: redis.Addr()}, time.UTC)
	started := make(chan struct{})
	go func() {
		replica.Start()
		close(started)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(replica.cron.Entries()) != len(periodicTasks) {
		if time.Now().After(deadline) {
			t.Fatalf("registered %d of %d periodic tasks", len(replica.cron.Entries()), len(periodicTasks))
		}
		time.Sleep(10 * time.Millisecond)
	}
	replica.Stop()
	replica.Stop()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}
