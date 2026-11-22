package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var _ kernel.OutboxRepo = (*outboxRepo)(nil)

type outboxRepo struct {
	db *gorm.DB
}

// NewOutboxRepo builds the module-owned implementation.
func NewOutboxRepo(db *gorm.DB) kernel.OutboxRepo {
	return &outboxRepo{db: db}
}

func (m *outboxRepo) Append(ctx context.Context, topic, eventKey, payload string) error {
	return m.db.WithContext(ctx).Create(&outbox.Event{
		Topic:        topic,
		EventKey:     eventKey,
		Payload:      payload,
		TraceCarrier: traceCarrier(ctx),
	}).Error
}

// traceCarrier serializes the caller's trace context so the event's later
// queue delivery can resume the producing request's trace; no active span
// means an empty carrier.
func traceCarrier(ctx context.Context) string {
	if !oteltrace.SpanContextFromContext(ctx).IsValid() {
		return ""
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return ""
	}
	serialized, err := json.Marshal(carrier)
	if err != nil {
		return ""
	}
	return string(serialized)
}

func (m *outboxRepo) ListUnpublished(ctx context.Context, limit int) ([]*outbox.Event, error) {
	var events []*outbox.Event
	err := m.db.WithContext(ctx).
		Where("published_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (m *outboxRepo) MarkPublished(ctx context.Context, id int64) error {
	return m.db.WithContext(ctx).Model(&outbox.Event{}).
		Where("id = ?", id).
		Update("published_at", timeutil.Now()).Error
}

func (m *outboxRepo) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := m.db.WithContext(ctx).
		Where("published_at IS NOT NULL AND published_at < ?", cutoff).
		Delete(&outbox.Event{})
	return result.RowsAffected, result.Error
}
