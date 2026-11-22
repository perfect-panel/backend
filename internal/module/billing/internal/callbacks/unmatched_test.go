package callbacks

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// memoryLog is the system log the callback flow records unmatched payments
// in; it searches content by substring as the repository does.
type memoryLog struct {
	rows      []*logEntity.SystemLog
	insertErr error
	searches  int
}

var _ UnmatchedPaymentLog = (*memoryLog)(nil)

func (m *memoryLog) Insert(_ context.Context, row *logEntity.SystemLog) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.rows = append(m.rows, row)
	return nil
}

func (m *memoryLog) FilterSystemLog(_ context.Context, filter *logEntity.FilterParams) ([]*logEntity.SystemLog, int64, error) {
	m.searches++
	var found []*logEntity.SystemLog
	for _, row := range m.rows {
		if filter.Type != 0 && row.Type != filter.Type {
			continue
		}
		if filter.ObjectID != 0 && row.ObjectID != filter.ObjectID {
			continue
		}
		if filter.Search != "" && !strings.Contains(row.Content, filter.Search) {
			continue
		}
		found = append(found, row)
	}
	return found, int64(len(found)), nil
}

// unmatched decodes the one record of the log.
func (m *memoryLog) unmatched(t *testing.T) (*logEntity.SystemLog, logEntity.UnmatchedPayment) {
	t.Helper()
	if len(m.rows) != 1 {
		t.Fatalf("unmatched payment records = %d, want exactly one", len(m.rows))
	}
	var recorded logEntity.UnmatchedPayment
	if err := recorded.Unmarshal([]byte(m.rows[0].Content)); err != nil {
		t.Fatal(err)
	}
	return m.rows[0], recorded
}

// A payment the gateway confirmed for an order that closed meanwhile is
// rejected as before, but first recorded once: the record names the order,
// the trade, the platform and what was collected, for the operator who
// refunds it; the gateway's redeliveries add nothing.
func TestNotifyRecordsAPaymentOfAClosedOrder(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	closed := pendingOrder(method, 1000, "CNY")
	closed.Status, closed.UserId = order.StatusClosed, 7
	orders := &callbackOrders{order: closed}
	logs := &memoryLog{}
	svc := NewService(orders, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(logs))
	ctx := requestmeta.With(paymentContext(method), requestmeta.New("203.0.113.5", "gateway"))

	for range 3 {
		err := svc.Notify(ctx, gateway.Notification{Params: signedEPayParams()})
		if err == nil || errors.Is(err, gateway.ErrInvalidCallback) {
			t.Fatalf("Notify = %v, want the rejection the gateway retries", err)
		}
	}
	row, recorded := logs.unmatched(t)
	if row.Type != logEntity.TypeUnmatchedPayment.Uint8() || row.ObjectID != 7 || row.Date == "" {
		t.Fatalf("record row = %+v, want an unmatched payment of user 7", row)
	}
	if recorded.OrderNo != "order-1" || recorded.TradeNo != "trade-1" || recorded.Platform != "EPay" || recorded.Amount != 1000 || recorded.Currency != "CNY" ||
		!strings.Contains(recorded.Reason, "closed") || recorded.Timestamp == 0 || recorded.ClientIP != "203.0.113.5" {
		t.Fatalf("record = %+v", recorded)
	}
	if orders.markCount != 0 {
		t.Fatal("the closed order was settled")
	}
}

// A payment of a finished order under another trade number is a second
// payment of the same order; a callback of a paid order bound to another
// trade likewise. Both are recorded.
func TestNotifyRecordsASecondPaymentOfAnOrder(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	for name, tt := range map[string]struct {
		status  uint8
		invalid bool
		reason  string
	}{
		"finished order":     {order.StatusFinished, true, "another trade"},
		"paid order":         {order.StatusPaid, false, "bound to another trade"},
		"pending with claim": {order.StatusPending, false, "bound to another trade"},
	} {
		t.Run(name, func(t *testing.T) {
			o := pendingOrder(method, 1000, "CNY")
			o.Status, o.TradeNo, o.UserId = tt.status, "trade-0", 7
			orders := &callbackOrders{order: o}
			logs := &memoryLog{}
			svc := NewService(orders, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(logs))

			err := svc.Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()})
			if err == nil || errors.Is(err, gateway.ErrInvalidCallback) != tt.invalid {
				t.Fatalf("Notify = %v, want a rejection with invalid = %t", err, tt.invalid)
			}
			_, recorded := logs.unmatched(t)
			if recorded.TradeNo != "trade-1" || !strings.Contains(recorded.Reason, tt.reason) {
				t.Fatalf("record = %+v, want trade-1 recorded for %q", recorded, tt.reason)
			}
			if orders.markCount != 0 || orders.order.TradeNo != "trade-0" {
				t.Fatal("the order's own trade was replaced")
			}
		})
	}
}

// A settled duplicate of the finished order's own trade is no unmatched
// payment, nor is a payment that settles.
func TestNotifyDoesNotRecordSettlementsOrDuplicates(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	logs := &memoryLog{}
	finished := pendingOrder(method, 1000, "CNY")
	finished.Status, finished.TradeNo = order.StatusFinished, "trade-1"
	if err := NewService(&callbackOrders{order: finished}, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(logs)).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err != nil {
		t.Fatalf("duplicate of the order's own trade: %v", err)
	}
	orders := &callbackOrders{order: pendingOrder(method, 1000, "CNY")}
	if err := NewService(orders, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(logs)).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err != nil || orders.markCount != 1 {
		t.Fatalf("settlement = %v, marks = %d", err, orders.markCount)
	}
	if len(logs.rows) != 0 {
		t.Fatalf("records = %d, want none", len(logs.rows))
	}
}

// A Cryptomus invoice the gateway asks an operator to review (underpaid,
// frozen, refunded) holds money for the order; the acknowledged lifecycle
// event is recorded for that operator, while a mere status update is not.
func TestNotifyRecordsAManualReviewInvoice(t *testing.T) {
	for status, recorded := range map[string]bool{"wrong_amount": true, "locked": true, "refund_paid": true, "check": false, "cancel": false} {
		t.Run(status, func(t *testing.T) {
			orders := &callbackOrders{order: &order.Order{
				OrderNo: "order-1", TradeNo: "uuid-1", UserId: 9, PaymentId: cryptomusMethodID, Method: "Cryptomus", Status: order.StatusPending,
				PaymentAmount: 1000, PaymentCurrency: "USD",
			}}
			logs := &memoryLog{}
			svc := NewService(orders, &fakeActivationQueue{}, cryptomusRegistry(":invalid-gateway"), WithUnmatchedPaymentLog(logs))
			payload := signCryptomusTestPayload(t, cryptomusAPIKey, map[string]any{
				"type": "payment", "uuid": "uuid-1", "order_id": "order-1",
				"amount": "10.00", "currency": "USD", "status": status, "is_final": true, "payment_amount": "5.00",
			})
			for range 2 {
				if err := svc.Notify(paymentContext(cryptomusPaymentConfig()), cryptomusNotification(payload)); err != nil {
					t.Fatalf("Notify: %v", err)
				}
			}
			if !recorded {
				if len(logs.rows) != 0 {
					t.Fatalf("records = %d, want none for status %s", len(logs.rows), status)
				}
				return
			}
			row, entry := logs.unmatched(t)
			if row.ObjectID != 9 || entry.Platform != "Cryptomus" || entry.TradeNo != "uuid-1" || entry.Amount != 1000 || entry.Currency != "USD" || !strings.Contains(entry.Reason, status) {
				t.Fatalf("record = %+v (row %+v)", entry, row)
			}
		})
	}
}

// A guest order has no user yet; its record belongs to no one (object 0)
// and is still found again on redelivery. A log store that fails, or none
// at all, leaves the rejection as it was.
func TestNotifyRecordsGuestOrdersAndSurvivesLogFailures(t *testing.T) {
	method := epayMethod(epayQueryServer(t, http.NotFound))
	guest := pendingOrder(method, 1000, "CNY")
	guest.Status = order.StatusClosed
	logs := &memoryLog{}
	svc := NewService(&callbackOrders{order: guest}, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(logs))
	for range 2 {
		if err := svc.Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err == nil {
			t.Fatal("the closed guest order was settled")
		}
	}
	if row, _ := logs.unmatched(t); row.ObjectID != 0 {
		t.Fatalf("record row = %+v, want object 0 for a guest order", row)
	}

	failing := &memoryLog{insertErr: errors.New("log store unavailable")}
	if err := NewService(&callbackOrders{order: guest}, &fakeActivationQueue{}, nil, WithUnmatchedPaymentLog(failing)).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err == nil {
		t.Fatal("the closed order was settled when the log store failed")
	}
	if err := NewService(&callbackOrders{order: guest}, &fakeActivationQueue{}, nil).Notify(paymentContext(method), gateway.Notification{Params: signedEPayParams()}); err == nil {
		t.Fatal("the closed order was settled without a log store")
	}
}

// The record of a payment is found by its order and trade, never by a trade
// of which the searched one is a substring.
func TestUnmatchedNeedleNamesOrderAndTrade(t *testing.T) {
	needle, err := unmatchedNeedle("order-1", `trade-"1`)
	if err != nil || needle != `"order_no":"order-1","trade_no":"trade-\"1"` {
		t.Fatalf("needle = %q, %v", needle, err)
	}
	content, err := (&logEntity.UnmatchedPayment{OrderNo: "order-1", TradeNo: `trade-"1`, Platform: "EPay"}).Marshal()
	if err != nil || !strings.Contains(string(content), needle) {
		t.Fatalf("stored content %s does not contain the needle %q", content, needle)
	}
}
