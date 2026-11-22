// Package taskqueue is what the producers and the consumers of the asynq
// task queue share: the task type names, the payloads, the task IDs that
// deduplicate enqueues, and the client and worker middleware that carry the
// OpenTelemetry trace context across the queue. Producers depend on these
// definitions, never on the consumers in internal/transport/task.
//
// asynq has no message headers, so the producer wraps the task payload in
// an envelope holding the W3C trace context, and the worker-side middleware
// unwraps it, resumes the producer's trace and opens a consumer span around
// the handler. Payloads without an envelope (older in-flight tasks,
// traceless producers, scheduler ticks) pass through untouched and get a
// root span, so every task execution logs a trace id either way.
package taskqueue

import (
	"context"
	"encoding/json"

	"github.com/pars-aria-labs/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Keep the instrumentation name stable across package moves.
const tracerName = "pkg/asynqx"

// envelope wraps a task payload with the producer's trace context. The key
// names are deliberately unmistakable so a business payload can never be
// misread as an envelope.
type envelope struct {
	Carrier map[string]string `json:"__trace_carrier__"`
	Body    json.RawMessage   `json:"__trace_body__"`
}

// Client wraps *asynq.Client so EnqueueContext stamps the caller's trace
// context onto the task. Wrapping rebuilds the task, which drops options
// embedded at NewTask time — pass options to EnqueueContext instead. The
// plain Enqueue promotes from the inner client untraced (it has no context
// to read).
type Client struct {
	*asynq.Client
}

// NewClient wraps client.
func NewClient(client *asynq.Client) *Client {
	return &Client{Client: client}
}

// EnqueueContext enqueues task with ctx's trace context in its payload.
func (c *Client) EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.Client.EnqueueContext(ctx, Wrap(ctx, task), opts...)
}

// Wrap returns a task whose payload carries ctx's trace context; without an
// active span (or with a payload the envelope cannot hold) it returns the
// task unchanged.
func Wrap(ctx context.Context, task *asynq.Task) *asynq.Task {
	if !oteltrace.SpanContextFromContext(ctx).IsValid() {
		return task
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return task
	}
	body := task.Payload()
	if len(body) == 0 {
		body = json.RawMessage("null")
	} else if !json.Valid(body) {
		return task
	}
	wrapped, err := json.Marshal(envelope{Carrier: carrier, Body: body})
	if err != nil {
		return task
	}
	return asynq.NewTask(task.Type(), wrapped)
}

// Middleware resumes the producer's trace from the payload envelope (when
// present), hands the handler the unwrapped payload, and records the task
// execution as a consumer span.
func Middleware() asynq.MiddlewareFunc {
	tracer := otel.Tracer(tracerName)
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			inner := task
			if payload := task.Payload(); len(payload) > 0 {
				var env envelope
				if err := json.Unmarshal(payload, &env); err == nil && env.Carrier != nil {
					ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(env.Carrier))
					body := []byte(env.Body)
					if string(body) == "null" {
						body = nil
					}
					inner = asynq.NewTask(task.Type(), body)
				}
			}
			attrs := []attribute.KeyValue{attribute.String("messaging.system", "asynq")}
			if id, ok := asynq.GetTaskID(ctx); ok {
				attrs = append(attrs, attribute.String("messaging.message.id", id))
			}
			if retried, ok := asynq.GetRetryCount(ctx); ok {
				attrs = append(attrs, attribute.Int("messaging.asynq.retry_count", retried))
			}
			ctx, span := tracer.Start(ctx, task.Type(),
				oteltrace.WithSpanKind(oteltrace.SpanKindConsumer),
				oteltrace.WithAttributes(attrs...))
			defer span.End()
			err := next.ProcessTask(ctx, inner)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return err
		})
	}
}
