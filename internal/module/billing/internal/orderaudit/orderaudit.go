// Package orderaudit writes the durable audit record created alongside an
// order. Callers invoke it with the transaction-scoped log repository so the
// order and its audit row commit or roll back together.
package orderaudit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// The sources an order audit record names as the order's creator.
const (
	SourceUser  = "user"
	SourceGuest = "guest"
	SourceAdmin = "admin"
)

// LogWriter appends to the audit log; the transaction-scoped log repository
// satisfies it.
type LogWriter interface {
	Insert(ctx context.Context, data *logEntity.SystemLog) error
}

// InsertCreated stores a safe order summary and request risk metadata.
func InsertCreated(ctx context.Context, logs LogWriter, data *order.Order, source string) error {
	if logs == nil {
		return errors.New("order audit log repository is unavailable")
	}
	if data == nil {
		return errors.New("order audit data is nil")
	}

	metadata, _ := requestmeta.From(ctx)
	now := timeutil.Now()
	content, err := (&logEntity.OrderCreated{
		Metadata:       metadata,
		OrderNo:        data.OrderNo,
		OrderType:      data.Type,
		Quantity:       data.Quantity,
		Price:          data.Price,
		Amount:         data.Amount,
		GiftAmount:     data.GiftAmount,
		Discount:       data.Discount,
		CouponDiscount: data.CouponDiscount,
		PaymentID:      data.PaymentId,
		Method:         data.Method,
		FeeAmount:      data.FeeAmount,
		SubscribeID:    data.SubscribeId,
		Source:         source,
		Timestamp:      now.UnixMilli(),
	}).Marshal()
	if err != nil {
		return fmt.Errorf("marshal order audit log: %w", err)
	}

	return logs.Insert(ctx, &logEntity.SystemLog{
		Type:     logEntity.TypeOrderCreated.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: data.UserId,
		Content:  string(content),
	})
}
