package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/order"
	"gorm.io/gorm"
)

const (
	orderEventCreated     = "order.created"
	orderEventPaymentPaid = "order.payment_paid"
	orderEventFulfilled   = "order.fulfilled"
	orderEventClosed      = "order.closed"
	orderEventStateChange = "order.state_changed"
)

// OrderEventRepo is deliberately separate from OrderRepo: order mutations
// write outbox rows atomically, while delivery workers and SSE handlers only
// need to read and mark these durable records.
type OrderEventRepo interface {
	FindOne(ctx context.Context, id int64) (*order.Event, error)
	ListAfter(ctx context.Context, orderNo string, afterID int64, limit int) ([]*order.Event, error)
	EarliestID(ctx context.Context, orderNo string) (int64, error)
	ListUnpublished(ctx context.Context, limit int) ([]*order.Event, error)
	MarkPublished(ctx context.Context, id int64, publishedAt time.Time) (bool, error)
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

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

func newOrderEventRepo(db *gorm.DB) OrderEventRepo {
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
		PaymentStatus:     orderPaymentStatus(data.Status),
		FulfillmentStatus: orderFulfillmentStatus(data.Status),
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
	case 2:
		return orderEventPaymentPaid
	case 3:
		return orderEventClosed
	case 5:
		return orderEventFulfilled
	default:
		return orderEventStateChange
	}
}

func orderPaymentStatus(status uint8) string {
	switch status {
	case 2, 5:
		return "paid"
	case 3:
		return "closed"
	case 4:
		return "failed"
	default:
		return "pending"
	}
}

func orderFulfillmentStatus(status uint8) string {
	switch status {
	case 5:
		return "finished"
	case 2:
		return "pending"
	case 3, 4:
		return "not_started"
	default:
		return "not_started"
	}
}
