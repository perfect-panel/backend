package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pars-aria-labs/asynq"
)

// publishingOutbox counts billing's outbox publications, failing them with
// err.
type publishingOutbox struct {
	runs int
	err  error
}

var _ orderEventPublisher = (*publishingOutbox)(nil)

func (o *publishingOutbox) PublishOrderEvents(context.Context) error {
	o.runs++
	return o.err
}

// cleanupRecorder records the runs of one retention cleanup, deleting one
// row per run or failing with err.
type cleanupRecorder struct {
	calls  int
	cutoff time.Time
	err    error
}

func (r *cleanupRecorder) run(cutoff time.Time) (int64, error) {
	r.calls++
	r.cutoff = cutoff
	return 1, r.err
}

// orderEventsCleanup stands in for billing's order-event cleanup, and
// outboxCleanup and inboxCleanup for the platform kernel's.
type (
	orderEventsCleanup struct{ cleanupRecorder }
	outboxCleanup      struct{ cleanupRecorder }
	inboxCleanup       struct{ cleanupRecorder }
)

var (
	_ orderEventCleaner     = (*orderEventsCleanup)(nil)
	_ PublishedEventPruner  = (*outboxCleanup)(nil)
	_ ProcessedMarkerPruner = (*inboxCleanup)(nil)
)

func (c *orderEventsCleanup) CleanupOrderEvents(_ context.Context, cutoff time.Time) (int64, error) {
	return c.run(cutoff)
}

func (c *outboxCleanup) DeletePublishedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	return c.run(cutoff)
}

func (c *inboxCleanup) DeleteProcessedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	return c.run(cutoff)
}

// The publisher task runs billing's outbox publication and hands its failure
// to asynq, which retries the task.
func TestPublishOrderEventsRunsTheBillingOutbox(t *testing.T) {
	outbox := &publishingOutbox{}
	handler := &PublishOrderEventsHandler{outbox: outbox}
	task := asynq.NewTask("test", nil)

	if err := handler.ProcessTask(context.Background(), task); err != nil || outbox.runs != 1 {
		t.Fatalf("run = %v with %d publications, want one", err, outbox.runs)
	}
	outbox.err = errors.New("redis unavailable")
	if err := handler.ProcessTask(context.Background(), task); !errors.Is(err, outbox.err) {
		t.Fatalf("failed run = %v, want the publication's error", err)
	}
}

// The daily cleanup applies the one 30-day replay contract to billing's order
// events and to the kernel's published domain events and inbox markers.
func TestCleanupOrderEventsAppliesTheReplayContract(t *testing.T) {
	orderEvents, outbox, inbox := &orderEventsCleanup{}, &outboxCleanup{}, &inboxCleanup{}
	handler := &CleanupOrderEventsHandler{orderEvents: orderEvents, outbox: outbox, inbox: inbox}
	const retention = 30 * 24 * time.Hour

	before := time.Now()
	if err := handler.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	after := time.Now()
	if cutoff := orderEvents.cutoff; orderEvents.calls != 1 || cutoff.Before(before.Add(-retention)) || cutoff.After(after.Add(-retention)) {
		t.Fatalf("order-event cleanup ran %d times with cutoff %v, want once %v ago", orderEvents.calls, cutoff, retention)
	}
	if outbox.calls != 1 || !outbox.cutoff.Equal(orderEvents.cutoff) || inbox.calls != 1 || !inbox.cutoff.Equal(orderEvents.cutoff) {
		t.Fatalf("kernel cleanups ran %d/%d times with cutoffs %v/%v, want once each at %v",
			outbox.calls, inbox.calls, outbox.cutoff, inbox.cutoff, orderEvents.cutoff)
	}
}

// The first failing cleanup ends the run with its error; the cleanups after
// it wait for asynq's retry.
func TestCleanupOrderEventsReturnsTheFirstFailure(t *testing.T) {
	failure := errors.New("database unavailable")
	names := []string{"order events", "outbox", "inbox"}
	for failing, name := range names {
		orderEvents, outbox, inbox := &orderEventsCleanup{}, &outboxCleanup{}, &inboxCleanup{}
		steps := []*cleanupRecorder{&orderEvents.cleanupRecorder, &outbox.cleanupRecorder, &inbox.cleanupRecorder}
		steps[failing].err = failure
		handler := &CleanupOrderEventsHandler{orderEvents: orderEvents, outbox: outbox, inbox: inbox}

		if err := handler.ProcessTask(context.Background(), asynq.NewTask("test", nil)); !errors.Is(err, failure) {
			t.Fatalf("%s failing: cleanup = %v, want its error", name, err)
		}
		for i, step := range steps {
			if ran := step.calls == 1; ran != (i <= failing) {
				t.Fatalf("%s failing: the %s cleanup ran %d times", name, names[i], step.calls)
			}
		}
	}
}
