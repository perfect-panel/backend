package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/eventbus"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// asynqEventPublisher puts domain events on the asynq queue. The task id is
// derived from the outbox event id, so a replayed enqueue (mark-published
// failed after a successful enqueue) hits the id conflict and is success,
// not an error; the retention window keeps the id claimed briefly after the
// delivery completes to widen that dedup.
type asynqEventPublisher struct {
	client *taskqueue.Client
}

func (p asynqEventPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	payload, err := json.Marshal(taskqueue.EventDeliverPayload{
		ID: event.ID, Topic: event.Topic, Key: event.Key, Payload: event.Payload,
	})
	if err != nil {
		return err
	}
	task := asynq.NewTask(taskqueue.EventDeliver, payload)
	// The delivery joins the ORIGINATING request's trace (stored on the
	// outbox row), not the publish pump's, so the wrap happens here with
	// the resumed origin context and the enqueue below goes through the
	// raw client to avoid re-stamping the pump's own span.
	task = taskqueue.Wrap(originContext(ctx, event.TraceCarrier), task)
	_, err = p.client.Client.EnqueueContext(ctx, task,
		asynq.TaskID(taskqueue.EventTaskID(event.ID)),
		asynq.Retention(time.Hour))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// originContext resumes the trace context serialized on the outbox row;
// rows without one (pre-trace events, traceless producers) fall back to the
// pump's context. The resumed trace is extracted into a fresh context: it
// only parents the delivery's span, and nothing of the pump's own context
// (its span, deadline or values) belongs to that trace.
func originContext(ctx context.Context, carrier string) context.Context {
	if carrier == "" {
		return ctx
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(carrier), &m); err != nil || len(m) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier(m))
}

// newEventBus wires the domain-event bus onto the asynq broker: producers
// append to the outbox inside their transactions; the queue's publish pump
// enqueues each event as an events:deliver task; the queue worker delivers
// it through these subscriptions. Handlers call module facades and rely on
// the modules' inbox idempotency.
func newEventBus(store repository.Store, srv *Application) *eventbus.Bus {
	bus := eventbus.New(store.Outbox(), asynqEventPublisher{client: srv.Queue})
	bus.Subscribe(identity.UserRegisteredTopic, subscription.TrialGrantConsumer, func(ctx context.Context, event eventbus.Event) error {
		userID, err := strconv.ParseInt(event.Key, 10, 64)
		if err == nil {
			return srv.Subscription.GrantTrial(ctx, userID)
		}
		// A key that is not a user id can never be granted a trial: failing
		// the delivery would only retry it into the dead-letter queue, so the
		// event is logged and dropped.
		logger.WithContext(ctx).Errorw("[EventBus] corrupt user_registered key; dropping", logger.Field("key", event.Key))
		return nil
	})
	return bus
}
