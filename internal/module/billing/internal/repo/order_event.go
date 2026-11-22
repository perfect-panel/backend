package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"gorm.io/gorm"
)

const (
	orderEventCreated     = "order.created"
	orderEventPaymentPaid = "order.payment_paid"
	orderEventFulfilled   = "order.fulfilled"
	orderEventClosed      = "order.closed"
	orderEventStateChange = "order.state_changed"
)

type orderEventRepo struct {
	db *gorm.DB
}

// withOrderEventTransaction keeps state and outbox writes atomic in normal
// execution. GORM DryRun is a SQL-generation mode with no live connection, so
// it cannot begin a transaction; running the callback directly preserves the
// generated statements for repository tests without weakening production.
func withOrderEventTransaction(conn *gorm.DB, fn func(*gorm.DB) error) error {
	if conn.DryRun {
		return fn(conn)
	}
	return conn.Transaction(fn)
}

// NewOrderEventRepo builds the module-owned implementation.
func NewOrderEventRepo(db *gorm.DB) repository.OrderEventRepo {
	return &orderEventRepo{db: db}
}

func (m *orderEventRepo) FindOne(ctx context.Context, id int64) (*order.Event, error) {
	var event order.Event
	err := m.db.WithContext(ctx).Where("id = ?", id).First(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (m *orderEventRepo) ListAfter(ctx context.Context, orderNo string, afterID int64, limit int) ([]*order.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var events []*order.Event
	err := m.db.WithContext(ctx).
		Where("order_no = ? AND id > ?", orderNo, afterID).
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (m *orderEventRepo) EarliestID(ctx context.Context, orderNo string) (int64, error) {
	var earliestID int64
	err := m.db.WithContext(ctx).
		Model(&order.Event{}).
		Where("order_no = ?", orderNo).
		Select("COALESCE(MIN(id), 0)").
		Scan(&earliestID).Error
	return earliestID, err
}

func (m *orderEventRepo) ListUnpublished(ctx context.Context, limit int) ([]*order.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var events []*order.Event
	err := m.db.WithContext(ctx).
		Where("published_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (m *orderEventRepo) MarkPublished(ctx context.Context, id int64, publishedAt time.Time) (bool, error) {
	result := m.db.WithContext(ctx).
		Model(&order.Event{}).
		Where("id = ? AND published_at IS NULL", id).
		Update("published_at", publishedAt)
	return result.RowsAffected == 1, result.Error
}

func (m *orderEventRepo) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := m.db.WithContext(ctx).
		Where("published_at IS NOT NULL AND created_at < ?", cutoff).
		Delete(&order.Event{})
	return result.RowsAffected, result.Error
}

type orderEventPayload struct {
	OrderNo           string `json:"order_no"`
	StateVersion      int64  `json:"state_version"`
	PaymentStatus     string `json:"payment_status"`
	FulfillmentStatus string `json:"fulfillment_status"`
}

func insertOrderEvent(conn *gorm.DB, data *order.Order, eventType string) error {
	payload, err := json.Marshal(orderEventPayload{
		OrderNo:           data.OrderNo,
		StateVersion:      data.StateVersion,
		PaymentStatus:     order.PaymentStatusName(data.Status),
		FulfillmentStatus: order.FulfillmentStatusName(data.Status),
	})
	if err != nil {
		return err
	}
	return conn.Create(&order.Event{
		OrderID:   data.Id,
		OrderNo:   data.OrderNo,
		EventType: eventType,
		Payload:   string(payload),
	}).Error
}

func orderEventTypeForStatus(status uint8) string {
	switch status {
	case order.StatusPaid:
		return orderEventPaymentPaid
	case order.StatusClosed:
		return orderEventClosed
	case order.StatusFinished:
		return orderEventFulfilled
	default:
		return orderEventStateChange
	}
}
