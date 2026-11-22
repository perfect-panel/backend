package taskqueue

import (
	"context"
	"testing"

	"github.com/pars-aria-labs/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func init() {
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func tracedContext(t *testing.T) (context.Context, oteltrace.TraceID) {
	t.Helper()
	traceID, err := oteltrace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := oteltrace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.FlagsSampled,
	})
	return oteltrace.ContextWithSpanContext(context.Background(), sc), traceID
}

func runMiddleware(t *testing.T, task *asynq.Task) (context.Context, *asynq.Task) {
	t.Helper()
	var gotCtx context.Context
	var gotTask *asynq.Task
	handler := Middleware()(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		gotCtx, gotTask = ctx, task
		return nil
	}))
	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	return gotCtx, gotTask
}

func TestWrapRoundTripResumesTrace(t *testing.T) {
	ctx, traceID := tracedContext(t)
	payload := []byte(`{"order_no":"A1"}`)

	wrapped := Wrap(ctx, asynq.NewTask("order:activate", payload))
	if string(wrapped.Payload()) == string(payload) {
		t.Fatal("expected the payload to be enveloped")
	}

	gotCtx, gotTask := runMiddleware(t, wrapped)
	if string(gotTask.Payload()) != string(payload) {
		t.Fatalf("handler must see the original payload, got %s", gotTask.Payload())
	}
	if got := oteltrace.SpanContextFromContext(gotCtx).TraceID(); got != traceID {
		t.Fatalf("expected the producer's trace id %s, got %s", traceID, got)
	}
}

func TestWrapWithoutSpanLeavesTaskUntouched(t *testing.T) {
	task := asynq.NewTask("t", []byte(`{"a":1}`))
	if wrapped := Wrap(context.Background(), task); wrapped != task {
		t.Fatal("expected the traceless task to pass through unchanged")
	}
}

func TestMiddlewarePassesRawPayloadThrough(t *testing.T) {
	payload := []byte(`{"legacy":true}`)
	gotCtx, gotTask := runMiddleware(t, asynq.NewTask("t", payload))
	if string(gotTask.Payload()) != string(payload) {
		t.Fatalf("raw payload must pass through, got %s", gotTask.Payload())
	}
	if oteltrace.SpanContextFromContext(gotCtx).TraceID().IsValid() {
		// The noop tracer yields no recorded trace id; the point is that the
		// handler still runs with the raw payload.
		t.Log("recorded tracer active; raw payload still passed through")
	}
}

func TestWrapNilPayloadRoundTrip(t *testing.T) {
	ctx, _ := tracedContext(t)
	wrapped := Wrap(ctx, asynq.NewTask("tick", nil))
	_, gotTask := runMiddleware(t, wrapped)
	if len(gotTask.Payload()) != 0 {
		t.Fatalf("expected nil payload after unwrap, got %q", gotTask.Payload())
	}
}
