package serverapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUserListBuildGateBoundsWaitersAndReleasesCanceledQueueSlots(t *testing.T) {
	gate := &userListBuildGate{active: make(chan struct{}, 1), waiting: make(chan struct{}, 1)}
	if err := gate.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- gate.acquire(ctx) }()
	deadline := time.After(time.Second)
	for len(gate.waiting) == 0 {
		select {
		case <-deadline:
			t.Fatal("fill did not queue")
		case <-time.After(time.Millisecond):
		}
	}
	if err := gate.acquire(t.Context()); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("full queue: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait: %v", err)
	}
	if len(gate.waiting) != 0 || len(gate.active) != 1 {
		t.Fatal("cancellation leaked queue capacity or released another worker")
	}
	gate.release()
	if err := gate.acquire(t.Context()); err != nil {
		t.Fatalf("capacity did not recover: %v", err)
	}
	gate.release()
}

func TestUserListBuildGateDeadlineIsRetryableAndDoesNotAcquireWorker(t *testing.T) {
	gate := &userListBuildGate{active: make(chan struct{}, 1), waiting: make(chan struct{}, 1)}
	if err := gate.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer gate.release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := gate.acquire(ctx); !errors.Is(err, ErrServerBusy) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait deadline: %v", err)
	}
	if len(gate.waiting) != 0 || len(gate.active) != 1 {
		t.Fatal("expired wait retained queue capacity or acquired a worker")
	}
}
