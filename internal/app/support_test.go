package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// The mirror pool runs the ticket events on a fixed number of workers and
// holds the rest in a bounded queue: a flood of replies never starts a
// goroutine each, and once the queue is full the excess is turned away.
func TestMirrorPoolBoundsConcurrencyAndDropsWhenFull(t *testing.T) {
	logtest.Discard(t)
	pool := newMirrorPool(2, 2)
	release := make(chan struct{})
	var (
		running, peak, done atomic.Int64
		started             sync.WaitGroup
	)
	started.Add(2)
	job := func(id int64) mirrorJob {
		return mirrorJob{ctx: context.Background(), ticketID: id, what: "reply", call: func(context.Context) error {
			if n := running.Add(1); n > peak.Load() {
				peak.Store(n)
			}
			if id <= 2 {
				started.Done()
			}
			<-release
			running.Add(-1)
			done.Add(1)
			return nil
		}}
	}
	// Two jobs occupy the workers, two wait in the queue, the fifth is
	// turned away.
	for id := int64(1); id <= 4; id++ {
		if !pool.submit(job(id)) {
			t.Fatalf("job %d was turned away with room in the pool", id)
		}
		if id == 2 {
			started.Wait()
		}
	}
	if pool.submit(job(5)) {
		t.Fatal("a job was accepted into a full queue")
	}
	if pool.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want the one turned away", pool.dropped.Load())
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for done.Load() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("ran %d jobs, want the four accepted", done.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if peak.Load() > 2 {
		t.Fatalf("peak concurrency = %d, want the two workers", peak.Load())
	}
}

// A job's context is the request's, detached from its cancellation, and
// bounded by the mirror timeout.
func TestMirrorPoolBoundsEachJob(t *testing.T) {
	logtest.Discard(t)
	pool := newMirrorPool(1, 1)
	got := make(chan context.Context, 1)
	pool.submit(mirrorJob{ctx: context.Background(), what: "status", call: func(ctx context.Context) error {
		got <- ctx
		return nil
	}})
	select {
	case ctx := <-got:
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > mirrorTimeout {
			t.Fatalf("job deadline = %v (%v), want within the mirror timeout", deadline, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not run")
	}
}
